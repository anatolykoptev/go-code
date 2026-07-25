package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/argnorm"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
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
	repos        []embeddings.RepoKeySourcePath
	reposErr     error
	listCalls    int
	listResult   []embeddings.PathCount
	listErr      error
	deleteResult int64
	deleteErr    error
	deleteCalls  int
}

func (f *fakeReconcileStore) ListRepoKeysWithSourcePath(ctx context.Context) ([]embeddings.RepoKeySourcePath, error) {
	return f.repos, f.reposErr
}

func (f *fakeReconcileStore) ListFilePathCounts(ctx context.Context, repoKey string) ([]embeddings.PathCount, error) {
	f.listCalls++
	return f.listResult, f.listErr
}

func (f *fakeReconcileStore) DeleteRowsByFilePaths(ctx context.Context, repoKey string, paths []string) (int64, error) {
	f.deleteCalls++
	return f.deleteResult, f.deleteErr
}

// TestHandleReconcilePaths_DefaultIsDryRun verifies that when DryRun is
// OMITTED (nil), the handler takes the preview path and does NOT call
// DeleteRowsByFilePaths.
//
// Falsifiable: reverting the default to delete makes
// fake.deleteCalls > 0 ⇒ assert.Zero fails.
func TestHandleReconcilePaths_DefaultIsDryRun(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))

	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_test", SourcePath: root},
		},
		listResult: []embeddings.PathCount{
			{Path: "keep.go", Count: 2},
			{Path: "gone.go", Count: 3},
		},
	}

	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError, "dry-run must not be an error result")

	assert.Equal(t, 1, fake.listCalls, "default must enumerate file paths")
	assert.Zero(t, fake.deleteCalls, "default (DryRun omitted) must NOT call DeleteRowsByFilePaths")

	text := textContentOf(t, res)
	assert.Contains(t, text, "DRY RUN", "default response must be a dry-run preview")
	assert.Contains(t, text, "stale_rows=3", "response must report the would-be-deleted row count")
	assert.Contains(t, text, "dry_run=false", "response must tell the operator how to force a real delete")
}

// TestHandleReconcilePaths_ExplicitDryRunFalseDeletes verifies that an
// explicit dry_run=false takes the real-delete path.
//
// Falsifiable: reverting the gate so dry_run=false still previews makes
// fake.deleteCalls == 0 ⇒ assert.Equal(1, ...) fails.
func TestHandleReconcilePaths_ExplicitDryRunFalseDeletes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))

	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_test", SourcePath: root},
		},
		listResult: []embeddings.PathCount{
			{Path: "keep.go", Count: 2},
			{Path: "gone.go", Count: 3},
		},
		deleteResult: 3,
	}

	dry := false
	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{DryRun: &dry}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	assert.Equal(t, 1, fake.deleteCalls, "dry_run=false must call DeleteRowsByFilePaths")
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
		listResult: []embeddings.PathCount{{Path: "a.go", Count: 1}},
	}

	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	assert.Zero(t, fake.deleteCalls, "empty source_path must not trigger a delete")
	text := textContentOf(t, res)
	assert.Contains(t, text, "skipped", "response must report the skip")
	assert.Contains(t, text, "source_path_empty")
}

// TestHandleReconcilePaths_RootMissingSkipsAndDeletesNothing verifies the
// data-loss guard at the MCP tool level: when a repo's source_path does not
// exist on disk, the handler skips it and deletes nothing.
func TestHandleReconcilePaths_RootMissingSkipsAndDeletesNothing(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.RemoveAll(root))

	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_dead", SourcePath: root},
		},
		listResult:   []embeddings.PathCount{{Path: "a.go", Count: 5}},
		deleteResult: 999, // should never be called
	}

	dry := false
	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{DryRun: &dry}, fake)
	require.NoError(t, err)
	require.False(t, res.IsError)

	assert.Zero(t, fake.deleteCalls, "root missing must not trigger a delete (data-loss guard)")
	text := textContentOf(t, res)
	assert.Contains(t, text, "skipped")
	assert.Contains(t, text, "root_missing")
}

// TestHandleReconcilePaths_IdempotentCleanRepo verifies that a second run on
// a clean repo reports 0 stale rows.
func TestHandleReconcilePaths_IdempotentCleanRepo(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))

	fake := &fakeReconcileStore{
		repos: []embeddings.RepoKeySourcePath{
			{RepoKey: "code_clean", SourcePath: root},
		},
		listResult: []embeddings.PathCount{{Path: "keep.go", Count: 5}},
	}

	res, err := handleReconcilePaths(context.Background(), ReconcilePathsInput{}, fake)
	require.NoError(t, err)
	text := textContentOf(t, res)
	assert.Contains(t, text, "stale_rows=0", "clean repo reports 0 stale rows")
	assert.Zero(t, fake.deleteCalls, "dry-run on clean repo does not delete")
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
