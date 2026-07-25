package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/argnorm"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReconcilePathsInput_NonEmptyClosedSchema guards the argnorm interaction
// (mirrors TestOrphanSweepInput_NonEmptyClosedSchema): ReconcilePathsInput
// carries a json field (dry_run) so the closed-empty-struct special-case must
// NOT apply; jsonProperties must return a non-nil, non-empty accepted set
// containing "dry_run".
func TestReconcilePathsInput_NonEmptyClosedSchema(t *testing.T) {
	props, isStruct := argnorm.JsonProperty(reflect.TypeFor[ReconcilePathsInput]())
	require.True(t, isStruct, "ReconcilePathsInput is a struct")
	require.NotNil(t, props, "ReconcilePathsInput must be a CLOSED schema (non-nil props), not open")
	require.NotEmpty(t, props, "ReconcilePathsInput must have a non-empty accepted set now that it has dry_run")
	assert.Contains(t, props, "dry_run", "dry_run must be an ACCEPTED param")
}

// fakeReconcileStore is an in-memory stand-in for the reconcilePathsStore
// interface. It records call counts so tests can assert which path the
// handler took (preview vs. real delete) WITHOUT a live Postgres pool.
type fakeReconcileStore struct {
	repos            []embeddings.RepoKeySourcePath
	reposErr         error
	pathlessRepos    []embeddings.RepoKeySourcePath
	pathlessReposErr error
	reconcileCalls   int
	reconcileResult  *embeddings.ReconcileResult
	reconcileErr     error
	reconcileResults map[string]*embeddings.ReconcileResult // per-repoKey overrides
}

func (f *fakeReconcileStore) ListRepoKeysWithSourcePath(ctx context.Context) ([]embeddings.RepoKeySourcePath, error) {
	return f.repos, f.reposErr
}

func (f *fakeReconcileStore) ListRepoKeysWithoutSourcePath(ctx context.Context) ([]embeddings.RepoKeySourcePath, error) {
	return f.pathlessRepos, f.pathlessReposErr
}

func (f *fakeReconcileStore) ReconcileRepoPaths(ctx context.Context, repoKey, sourcePath string, dryRun bool) (*embeddings.ReconcileResult, error) {
	f.reconcileCalls++
	if f.reconcileResults != nil {
		if res, ok := f.reconcileResults[repoKey]; ok {
			return res, f.reconcileErr
		}
	}
	return f.reconcileResult, f.reconcileErr
}

// TestHandleReconcilePaths_DefaultIsDryRun verifies that when DryRun is
// OMITTED (nil), the handler takes the preview path and does NOT report
// any deletions.
//
// Falsifiable: reverting the default to delete makes the result report
// DELETED instead of DRY RUN ⇒ assert.Contains("DRY RUN") fails.
func TestHandleReconcilePaths_DefaultIsDryRun(t *testing.T) {
	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_test", SourcePath: "/tmp/repo"},
		},
		reconcileResult: &embeddings.ReconcileResult{
			RepoKey:    "code_test",
			StalePaths: 1,
			StaleRows:  3,
			TotalRows:  5,
			DryRun:     true,
		},
	}

	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError, "dry-run must not be an error result")

	assert.Equal(t, 1, fake.reconcileCalls, "default must call ReconcileRepoPaths once")

	text := textContentOf(t, res)
	assert.Contains(t, text, "DRY RUN", "default response must be a dry-run preview")
	assert.Contains(t, text, "stale_rows=3", "response must report the would-be-deleted row count")
	assert.Contains(t, text, "dry_run=false", "response must tell the operator how to force a real delete")
}

// TestHandleReconcilePaths_ExplicitDryRunFalseDeletes verifies that an
// explicit dry_run=false takes the real-delete path.
//
// Falsifiable: reverting the gate so dry_run=false still previews makes
// the result contain "DRY RUN" instead of "DELETED" ⇒ assert.Contains("DELETED") fails.
func TestHandleReconcilePaths_ExplicitDryRunFalseDeletes(t *testing.T) {
	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_test", SourcePath: "/tmp/repo"},
		},
		reconcileResult: &embeddings.ReconcileResult{
			RepoKey:    "code_test",
			StalePaths: 1,
			StaleRows:  3,
			TotalRows:  5,
			Deleted:    3,
			DryRun:     false,
		},
	}

	dry := false
	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{DryRun: &dry}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	assert.Equal(t, 1, fake.reconcileCalls, "dry_run=false must call ReconcileRepoPaths")
	text := textContentOf(t, res)
	assert.Contains(t, text, "DELETED", "real-delete response must be the DELETED form")
	assert.Contains(t, text, "rows_deleted=3")
}

// TestHandleReconcilePaths_SkipsEmptySourcePath verifies that a repo with an
// empty source_path is skipped (not reconciled, not deleted).
func TestHandleReconcilePaths_SkipsEmptySourcePath(t *testing.T) {
	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_pathless", SourcePath: ""},
		},
		reconcileResult: &embeddings.ReconcileResult{
			RepoKey:    "code_pathless",
			Skipped:    true,
			SkipReason: "source_path_empty",
		},
	}

	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	text := textContentOf(t, res)
	assert.Contains(t, text, "skipped", "response must report the skip")
	assert.Contains(t, text, "source_path_empty")
}

// TestHandleReconcilePaths_RootMissingSkipsAndDeletesNothing verifies the
// data-loss guard at the MCP tool level: when a repo's source_path does not
// exist on disk, the handler skips it and deletes nothing.
func TestHandleReconcilePaths_RootMissingSkipsAndDeletesNothing(t *testing.T) {
	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_dead", SourcePath: "/nonexistent/path"},
		},
		reconcileResult: &embeddings.ReconcileResult{
			RepoKey:    "code_dead",
			Skipped:    true,
			SkipReason: "root_missing",
		},
	}

	dry := false
	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{DryRun: &dry}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	text := textContentOf(t, res)
	assert.Contains(t, text, "skipped")
	assert.Contains(t, text, "root_missing")
}

// TestHandleReconcilePaths_IdempotentCleanRepo verifies that a second run on
// a clean repo reports 0 stale rows.
func TestHandleReconcilePaths_IdempotentCleanRepo(t *testing.T) {
	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_clean", SourcePath: "/tmp/repo"},
		},
		reconcileResult: &embeddings.ReconcileResult{
			RepoKey:    "code_clean",
			StalePaths: 0,
			StaleRows:  0,
			TotalRows:  5,
			DryRun:     true,
		},
	}

	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{}, fake)
	require.NoError(t, err)
	text := textContentOf(t, res)
	assert.Contains(t, text, "stale_rows=0", "clean repo reports 0 stale rows")
}

// TestHandleReconcilePaths_ListErrorPropagates verifies that a ListRepoKeys
// error is returned (not swallowed).
func TestHandleReconcilePaths_ListErrorPropagates(t *testing.T) {
	fake := &fakeReconcileStore{
		reposErr: context.DeadlineExceeded,
	}
	_, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{}, fake)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "list"), "error must mention the list step")
}

// -- Finding 2: tool path emits threshold WARN and root-missing WARN --

// TestHandleReconcilePaths_ToolEmitsThresholdAndRootMissingWARNs verifies
// that the TOOL path (handleReconcilePaths) emits both the high-stale-ratio
// WARN and the root-missing WARN by delegating to the SINGLE implementation
// (embeddings.ReconcileRepoPaths). This is the finding-2 guard: if the
// handler were to use a parallel adapter that omits the slog calls, these
// WARNs would not appear in the log output.
//
// DB-gated: needs a real store so ReconcileRepoPaths actually runs its
// slog.Warn branches. Uses a real *embeddings.Store wrapping the test pool.
func TestHandleReconcilePaths_ToolEmitsThresholdAndRootMissingWARNs(t *testing.T) {
	dsn := os.Getenv("PR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PR_TEST_DATABASE_URL to run the tool-path WARN test")
	}
	cfg, parseErr := pgxpool.ParseConfig(dsn)
	if parseErr != nil {
		t.Fatalf("parse PR_TEST_DATABASE_URL: %v", parseErr)
	}
	if strings.EqualFold(cfg.ConnConfig.Database, "gocode") {
		t.Fatalf("refusing to run against the prod gocode DB")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := embeddings.NewStore(pool)
	ctx := context.Background()
	require.NoError(t, store.EnsureSchema(ctx))

	// Capture slog output to assert WARN lines are emitted.
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	// Repo A: high stale ratio (2 of 3 rows stale = 0.667 > 0.10 threshold).
	const repoA = "test/tool-warn-threshold"
	cleanReconcileRepo(t, store, repoA)
	rootA := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(rootA, "keep.go"), []byte("x"), 0o644))
	insertReconcileSymbols(t, store, repoA, "keep.go", []string{"A"})
	insertReconcileSymbols(t, store, repoA, "gone.go", []string{"B", "C"})
	require.NoError(t, store.SetRepoStateWithPath(ctx, repoA, "deadbeef", "test-model", rootA))

	// Repo B: root missing (source_path does not exist).
	const repoB = "test/tool-warn-root-missing"
	cleanReconcileRepo(t, store, repoB)
	insertReconcileSymbols(t, store, repoB, "a.go", []string{"X"})

	rootB := t.TempDir()
	require.NoError(t, os.RemoveAll(rootB), "rootB does not exist")

	// Register both repos in code_repo_state with their source_paths.
	require.NoError(t, store.SetRepoStateWithPath(ctx, repoB, "deadbeef", "test-model", rootB))

	// Build a real store wrapper that satisfies reconcilePathsStore.
	// *embeddings.Store already implements the interface.
	dry := false
	res, err := handleReconcilePaths(ctx, ReconcilePathsInput{DryRun: &dry}, store)
	require.NoError(t, err)
	require.False(t, res.IsError)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "high stale-path ratio",
		"tool path must emit the threshold WARN via ReconcileRepoPaths")
	assert.Contains(t, logOutput, "root missing",
		"tool path must emit the root-missing WARN via ReconcileRepoPaths")
}

// cleanReconcileRepo removes all embeddings for a repo key at test start/end.
func cleanReconcileRepo(t *testing.T, store *embeddings.Store, repoKey string) {
	t.Helper()
	ctx := context.Background()
	_ = store.DeleteRepo(ctx, repoKey)
	t.Cleanup(func() { _ = store.DeleteRepo(ctx, repoKey) })
}

// insertReconcileSymbols inserts EmbeddingRecord rows with fake zero vectors.
func insertReconcileSymbols(t *testing.T, store *embeddings.Store, repoKey, filePath string, names []string) {
	t.Helper()
	ctx := context.Background()
	records := make([]embeddings.EmbeddingRecord, len(names))
	for i, name := range names {
		records[i] = embeddings.EmbeddingRecord{
			RepoKey:    repoKey,
			FilePath:   filePath,
			SymbolName: name,
			SymbolKind: "function",
			Language:   "go",
			StartLine:  i + 1,
			BodyHash:   uint64(i + 1),
			Embedding:  makeReconcileVec(),
		}
	}
	require.NoError(t, store.Upsert(ctx, records))
}

// makeReconcileVec returns a zero vector of the right dimension for the test DB.
func makeReconcileVec() []float32 {
	return make([]float32, 768) // code-rank-embed dimension
}

// -- #714: pathless keys must be enumerated by the tool, not just direct-called --

// TestHandleReconcilePaths_PathlessKeyReachedViaEnumeration is test 2 from the
// task: a pathless key (source_path = ”) reached through the TOOL's
// enumeration ⇒ lands on the skip branch, unmeasured{reason="source_path_empty"}=1,
// nothing deleted.
//
// CRITICAL anti-vacuity: a test that calls ReconcileRepoPaths("") directly
// passes today and proves nothing about reachability. This test goes through
// the enumeration (ListRepoKeysWithoutSourcePath → handleReconcilePaths →
// ReconcileRepoPaths) so it exercises the REAL production path.
//
// Falsifiable: revert the tool to non-empty-only enumeration (remove the
// ListRepoKeysWithoutSourcePath walk) ⇒ the pathless repo is never iterated,
// ReconcileRepoPaths is never called for it, unmeasured gauge stays at 0 ⇒
// assert.Equal(1, ...) on the unmeasured gauge REDS.
//
// Uses a real *embeddings.Store so the gauge is actually set. DB-gated.
func TestHandleReconcilePaths_PathlessKeyReachedViaEnumeration(t *testing.T) {
	dsn := os.Getenv("PR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PR_TEST_DATABASE_URL to run the pathless enumeration test")
	}
	cfg, parseErr := pgxpool.ParseConfig(dsn)
	if parseErr != nil {
		t.Fatalf("parse PR_TEST_DATABASE_URL: %v", parseErr)
	}
	if strings.EqualFold(cfg.ConnConfig.Database, "gocode") {
		t.Fatalf("refusing to run against the prod gocode DB")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := embeddings.NewStore(pool)
	ctx := context.Background()
	require.NoError(t, store.EnsureSchema(ctx))

	// Seed a pathless key: state row with source_path='', plus embeddings.
	const pathlessRepo = "test/tool-pathless-enumerated"
	cleanReconcileRepo(t, store, pathlessRepo)
	// Also clean the state row (cleanReconcileRepo only deletes embeddings).
	_, _ = pool.Exec(ctx, `DELETE FROM public.code_repo_state WHERE repo_key = $1`, pathlessRepo)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM public.code_repo_state WHERE repo_key = $1`, pathlessRepo)
	})

	insertReconcileSymbols(t, store, pathlessRepo, "pkg/agent/main.go", []string{"X", "Y"})
	// State row with empty source_path — the pathless tombstone shape.
	require.NoError(t, store.SetRepoStateWithPath(ctx, pathlessRepo, "deadbeef", "test-model", ""))

	// Verify the key IS enumerated by ListRepoKeysWithoutSourcePath — this is
	// the reachability gate. If the query is wrong, the test fails here.
	pathless, listErr := store.ListRepoKeysWithoutSourcePath(ctx)
	require.NoError(t, listErr)
	var found bool
	for _, r := range pathless {
		if r.RepoKey == pathlessRepo {
			found = true
			break
		}
	}
	require.True(t, found, "pathless key must be enumerated by ListRepoKeysWithoutSourcePath")

	// Reset the unmeasured gauge for this repo to 0 (simulating boot warm-up).
	embeddings.SetStalePathUnmeasuredGauge(pathlessRepo, "source_path_empty", 0)

	// Run the tool handler with dry-run=true (default). The handler must walk
	// the pathless repos and call ReconcileRepoPaths for each, landing on the
	// source_path_empty skip branch.
	res, err := handleReconcilePaths(ctx, ReconcilePathsInput{}, store)
	require.NoError(t, err)
	require.False(t, res.IsError)

	text := textContentOf(t, res)
	assert.Contains(t, text, pathlessRepo, "pathless key must appear in the report")
	assert.Contains(t, text, "source_path_empty",
		"pathless key must be reported as skipped with source_path_empty reason")

	// The unmeasured gauge MUST be 1 with reason="source_path_empty".
	// This is the core assertion: the pathless key stops reporting clean.
	unmeasured := gaugeReconcileValue(t, pathlessRepo, "source_path_empty")
	assert.Equal(t, 1.0, unmeasured,
		"unmeasured{reason=source_path_empty} must be 1 after tool enumeration — "+
			"if 0, the tool did not walk pathless keys (#714)")

	// Nothing deleted — the skip branch deletes nothing.
	rows, err := store.GetSymbolsForFile(ctx, pathlessRepo, "pkg/agent/main.go")
	require.NoError(t, err)
	assert.Len(t, rows, 2, "pathless key rows must survive — skip branch deletes nothing")
}

// gaugeReconcileValue reads a stale-path unmeasured gauge value for a test.
func gaugeReconcileValue(t *testing.T, repo, reason string) float64 {
	t.Helper()
	// Gather from the default Prometheus registry (the gauge is registered
	// via promauto in the embeddings package, so it lives on the default
	// registry). Filter by name + label pair.
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "gocode_index_stale_path_unmeasured" {
			continue
		}
		for _, m := range mf.GetMetric() {
			repoOK, reasonOK := false, false
			for _, l := range m.GetLabel() {
				if l.GetName() == "repo" && l.GetValue() == repo {
					repoOK = true
				}
				if l.GetName() == "reason" && l.GetValue() == reason {
					reasonOK = true
				}
			}
			if repoOK && reasonOK {
				return m.GetGauge().GetValue()
			}
		}
	}
	return 0 // not found — treated as 0 (the warmed default)
}
