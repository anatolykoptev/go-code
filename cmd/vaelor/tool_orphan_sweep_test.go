package main

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/argnorm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrphanSweepInput_NonEmptyClosedSchema guards the argnorm interaction
// (#581b): OrphanSweepInput is NO LONGER struct{} — it now carries a json
// field (dry_run). The closed-empty-struct special-case must therefore NOT
// apply to it; instead jsonProperties must return a non-nil, non-empty
// accepted set containing "dry_run" (closed schema, dry_run is an ACCEPTED
// param, not stripped). The synthetic struct{} empty-struct path stays
// covered by internal/argnorm's own TestRegistry_ClosedEmptyStructNotOpen /
// TestJsonProperties_StructEmptyIsClosed (which use a local emptyStruct type,
// not OrphanSweepInput).
//
// Falsifiable: reverting OrphanSweepInput to struct{} makes len(props)==0 →
// the dry_run assertion fails. Removing the json tag makes props nil (open
// schema) → the non-nil assertion fails.
func TestOrphanSweepInput_NonEmptyClosedSchema(t *testing.T) {
	props, isStruct := argnorm.JsonProperty(reflect.TypeFor[OrphanSweepInput]())
	require.True(t, isStruct, "OrphanSweepInput is a struct")
	require.NotNil(t, props, "OrphanSweepInput must be a CLOSED schema (non-nil props), not open")
	require.NotEmpty(t, props, "OrphanSweepInput must have a non-empty accepted set now that it has dry_run")
	assert.Contains(t, props, "dry_run", "dry_run must be an ACCEPTED param")
}

// fakeOrphanSweepStore is an in-memory stand-in for the orphanSweepStore
// interface. It records call counts so tests can assert which path the
// handler took (preview vs. real delete) WITHOUT a live Postgres pool.
//
// perKeyRows maps repoKey → orphan row count returned by both
// CountOrphanRepoKeysForRepo and DeleteOrphanRepoKeysForRepo, so the per-key
// count and the per-key delete agree by construction (the guard for #741
// requirement 1: preview/count/delete must not diverge).
type fakeOrphanSweepStore struct {
	previewKeys  []string
	previewRows  int64
	previewErr   error
	previewCalls int

	countResult int64
	countErr    error
	countCalls  int

	// per-key state
	perKeyRows      map[string]int64 // shared by Count/Delete ForRepo
	perKeyCountErr  error
	perKeyDeleteErr error

	deletedKeys   []string // repoKeys actually passed to DeleteOrphanRepoKeysForRepo
	countedKeys   []string // repoKeys actually passed to CountOrphanRepoKeysForRepo
	perKeyCallsMu sync.Mutex
}

func (f *fakeOrphanSweepStore) PreviewOrphanRepoKeys(ctx context.Context) ([]string, int64, error) {
	f.previewCalls++
	return f.previewKeys, f.previewRows, f.previewErr
}

func (f *fakeOrphanSweepStore) CountOrphanRepoKeys(ctx context.Context) (int64, error) {
	f.countCalls++
	return f.countResult, f.countErr
}

func (f *fakeOrphanSweepStore) CountOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error) {
	f.perKeyCallsMu.Lock()
	defer f.perKeyCallsMu.Unlock()
	f.countedKeys = append(f.countedKeys, repoKey)
	if f.perKeyRows == nil {
		return 0, f.perKeyCountErr
	}
	return f.perKeyRows[repoKey], f.perKeyCountErr
}

func (f *fakeOrphanSweepStore) DeleteOrphanRepoKeysForRepo(ctx context.Context, repoKey string) (int64, error) {
	f.perKeyCallsMu.Lock()
	defer f.perKeyCallsMu.Unlock()
	f.deletedKeys = append(f.deletedKeys, repoKey)
	if f.perKeyRows == nil {
		return 0, f.perKeyDeleteErr
	}
	return f.perKeyRows[repoKey], f.perKeyDeleteErr
}

// fakeSlotClaimer is an in-memory indexSlotClaimer. perKeyWon maps repoKey →
// whether ClaimIndexSlot wins (true) or loses to an in-flight indexer (false).
// releases records every release call so a test can assert no claim is leaked.
type fakeSlotClaimer struct {
	mu        sync.Mutex
	perKeyWon map[string]bool
	releases  []string // repoKeys whose release was called
}

func (c *fakeSlotClaimer) ClaimIndexSlot(repoKey string) (release func(), won bool) {
	c.mu.Lock()
	won = c.perKeyWon[repoKey]
	c.mu.Unlock()
	if !won {
		return nil, false
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			c.mu.Lock()
			c.releases = append(c.releases, repoKey)
			c.mu.Unlock()
		})
	}
	return release, true
}

func (c *fakeSlotClaimer) released(repoKey string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range c.releases {
		if k == repoKey {
			return true
		}
	}
	return false
}

// TestHandleOrphanSweep_DefaultIsDryRun is the primary falsifiable guard for
// the safe-default gate: when DryRun is OMITTED (nil), the handler must take
// the preview path and must NOT delete anything.
//
// Falsifiable: reverting the default to delete makes the response contain
// "DELETED" not "DRY RUN", and deletedKeys is non-empty.
func TestHandleOrphanSweep_DefaultIsDryRun(t *testing.T) {
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/orphan-a", "test/orphan-b"},
		previewRows: 15076,
		perKeyRows:  map[string]int64{"test/orphan-a": 7000, "test/orphan-b": 8076},
	}
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{"test/orphan-a": true, "test/orphan-b": true}}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{}, fake, claimer)
	require.NoError(t, err)
	require.False(t, res.IsError, "dry-run must not be an error result")

	assert.Equal(t, 1, fake.previewCalls, "default must invoke the preview path")
	assert.Empty(t, fake.deletedKeys, "default (DryRun omitted) must NOT delete anything")

	text := textContentOf(t, res)
	assert.Contains(t, text, "DRY RUN", "default response must be a dry-run preview")
	assert.Contains(t, text, "orphan_repo_keys=2", "response must report the orphan key count")
	assert.Contains(t, text, "rows_that_would_be_deleted=15076", "response must report the would-be-deleted row count")
	assert.Contains(t, text, "dry_run=false", "response must tell the operator how to force a real delete")
}

// TestHandleOrphanSweep_ExplicitDryRunTrue verifies that an explicit
// dry_run=true takes the preview path (same as the default), with no mutation.
func TestHandleOrphanSweep_ExplicitDryRunTrue(t *testing.T) {
	dry := true
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/orphan-x"},
		previewRows: 42,
		perKeyRows:  map[string]int64{"test/orphan-x": 42},
	}
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{"test/orphan-x": true}}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake, claimer)
	require.NoError(t, err)
	require.False(t, res.IsError)

	assert.Equal(t, 1, fake.previewCalls, "dry_run=true must invoke the preview path")
	assert.Empty(t, fake.deletedKeys, "dry_run=true must NOT delete anything")

	assert.Contains(t, textContentOf(t, res), "DRY RUN")
}

// TestHandleOrphanSweep_InFlightKeyExcludedNotDeleted is #741 test 1.
//
// A repoKey whose index slot is already claimed by an in-flight indexer
// (claimer returns won=false) must NOT have its rows deleted, and must appear
// in the sweep output as excluded. This is the core race fix: on main the
// handler has no claimer and deletes every orphan key, so this test reddens
// on main (the in-flight key's rows are deleted and it is not reported as
// excluded).
//
// Falsifiable: revert the guard (handler deletes per-key without checking
// won) → fake.deletedKeys contains "test/inflight" and the response no longer
// contains "excluded_in_flight=1". The "handler calls the bulk
// DeleteOrphanRepoKeys" arm this note previously described can no longer be
// armed: DeleteOrphanRepoKeys was removed from the handler's
// orphanSweepStore interface (finding 2), so reverting to a bulk delete would
// not compile — the type makes that regression impossible, which is the
// point of narrowing the interface.
func TestHandleOrphanSweep_InFlightKeyExcludedNotDeleted(t *testing.T) {
	dry := false
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/inflight", "test/orphan-clean"},
		previewRows: 200,
		perKeyRows: map[string]int64{
			"test/inflight":     150,
			"test/orphan-clean": 50,
		},
		countResult: 2, // before-count
	}
	// "test/inflight" is claimed by an indexer → sweep must lose the claim.
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{
		"test/inflight":     false,
		"test/orphan-clean": true,
	}}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake, claimer)
	require.NoError(t, err)
	require.False(t, res.IsError)

	// The in-flight key must NOT be deleted.
	assert.NotContains(t, fake.deletedKeys, "test/inflight",
		"in-flight repoKey must NOT be deleted — an indexer owns its slot")
	// The clean key must be deleted.
	assert.Contains(t, fake.deletedKeys, "test/orphan-clean",
		"unclaimed orphan repoKey must still be swept")

	text := textContentOf(t, res)
	assert.Contains(t, text, "DELETED")
	assert.Contains(t, text, "excluded_in_flight=1",
		"response must report the count of excluded (in-flight) keys")
	assert.Contains(t, text, "test/inflight",
		"response must name the excluded key — a silent skip is the mirror of the bug")
}

// TestHandleOrphanSweep_UnclaimedKeyStillSwept is #741 test 2 (regression
// guard). When no indexer holds any slot, the sweep must still delete every
// orphan key — the guard must not turn the sweep into a no-op.
//
// Falsifiable: if the guard is wired backwards (skip when won=true), no key is
// deleted and rows_deleted=0.
func TestHandleOrphanSweep_UnclaimedKeyStillSwept(t *testing.T) {
	dry := false
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/a", "test/b", "test/c"},
		previewRows: 300,
		perKeyRows: map[string]int64{
			"test/a": 100,
			"test/b": 100,
			"test/c": 100,
		},
		countResult: 3,
	}
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{
		"test/a": true, "test/b": true, "test/c": true,
	}}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake, claimer)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"test/a", "test/b", "test/c"}, fake.deletedKeys,
		"all unclaimed orphan keys must be swept")
	text := textContentOf(t, res)
	assert.Contains(t, text, "rows_deleted=300")
	assert.Contains(t, text, "excluded_in_flight=0")
}

// TestHandleOrphanSweep_PreviewCountDeleteAgree is #741 test 3 — the guard
// for requirement 1. Under the SAME in-flight state, the dry-run preview, the
// per-key count, and the real delete must report the same eligible candidate
// set and the same row counts. Constructed so a future change that updates one
// predicate (or one path's guard) and not the others reddens here.
//
// Setup: two orphan keys, one in-flight. Dry run reports the in-flight key as
// a candidate (dry run does NOT claim — see DRY-RUN DECISION) but flags that
// it would be excluded at delete time; the real delete excludes it. The
// per-key count and per-key delete for the eligible key must agree on row
// count. The eligible set (keys actually deleted) must equal the set the
// guard admits.
func TestHandleOrphanSweep_PreviewCountDeleteAgree(t *testing.T) {
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/guarded", "test/inflight"},
		previewRows: 250,
		perKeyRows: map[string]int64{
			"test/guarded":  200,
			"test/inflight": 50,
		},
		countResult: 2,
	}
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{
		"test/guarded":  true,
		"test/inflight": false,
	}}

	// Dry run: does not claim, reports the full candidate set, notes the guard
	// applies at delete time.
	dry := true
	dryRes, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake, claimer)
	require.NoError(t, err)
	dryText := textContentOf(t, dryRes)
	assert.Contains(t, dryText, "orphan_repo_keys=2")
	assert.Contains(t, dryText, "rows_that_would_be_deleted=250")
	assert.Contains(t, dryText, "in-flight",
		"dry run must note that in-flight keys are excluded only at delete time")

	// Reset per-key call records.
	fake.deletedKeys = nil
	fake.countedKeys = nil

	// Real delete: excludes the in-flight key, deletes the guarded key.
	del := false
	delRes, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &del}, fake, claimer)
	require.NoError(t, err)
	delText := textContentOf(t, delRes)

	// The eligible key was counted then deleted — count and delete saw the
	// same repoKey and the same row count (perKeyRows is shared).
	assert.Contains(t, fake.countedKeys, "test/guarded")
	assert.Contains(t, fake.deletedKeys, "test/guarded")
	assert.NotContains(t, fake.deletedKeys, "test/inflight")
	assert.Contains(t, delText, "rows_deleted=200",
		"rows_deleted must equal the per-key count for the eligible key")
	assert.Contains(t, delText, "excluded_in_flight=1")
}

// TestHandleOrphanSweep_ClaimReleasedAfterSweep is #741 test 4 — catches the
// leaked-claim failure, which is silent and permanent (a leaked claim blocks
// that repoKey from ever indexing again for the process lifetime). After the
// sweep deletes a key, the claim it won MUST be released so a subsequent
// index can claim the slot.
//
// Falsifiable: if the handler forgets to call release on the success path
// (e.g. deletes then returns without releasing), claimer.released is empty for
// that key → assert.True fails. The error-path variant
// (TestHandleOrphanSweep_ClaimReleasedOnDeleteError) covers the case where
// Delete returns an error and the handler must still release.
func TestHandleOrphanSweep_ClaimReleasedAfterSweep(t *testing.T) {
	dry := false
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/releaseme"},
		previewRows: 99,
		perKeyRows:  map[string]int64{"test/releaseme": 99},
		countResult: 1,
	}
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{"test/releaseme": true}}

	_, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake, claimer)
	require.NoError(t, err)

	assert.True(t, claimer.released("test/releaseme"),
		"claim must be released after the sweep so the repoKey can be indexed again — a leaked claim is silent and permanent")
}

// TestHandleOrphanSweep_ClaimReleasedOnDeleteError is the error-path companion
// to test 4: a delete error must NOT leak the claim. The handler must release
// every claim it won, including on the error path (defer per key, not one
// defer for the loop — requirement 3).
func TestHandleOrphanSweep_ClaimReleasedOnDeleteError(t *testing.T) {
	dry := false
	fake := &fakeOrphanSweepStore{
		previewKeys:     []string{"test/errkey"},
		previewRows:     1,
		perKeyRows:      map[string]int64{"test/errkey": 1},
		countResult:     1,
		perKeyDeleteErr: context.DeadlineExceeded,
	}
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{"test/errkey": true}}

	_, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake, claimer)
	require.Error(t, err)

	assert.True(t, claimer.released("test/errkey"),
		"claim must be released even when the per-key delete errors — defer per key, not one defer for the loop")
}

// TestHandleOrphanSweep_DryRunPreviewErrorPropagates verifies that a preview
// error is returned (the handler must not swallow it and fall through to delete).
func TestHandleOrphanSweep_DryRunPreviewErrorPropagates(t *testing.T) {
	fake := &fakeOrphanSweepStore{
		previewErr: context.DeadlineExceeded,
	}
	claimer := &fakeSlotClaimer{perKeyWon: map[string]bool{}}

	_, err := handleOrphanSweep(context.Background(), OrphanSweepInput{}, fake, claimer)
	require.Error(t, err)
	assert.Empty(t, fake.deletedKeys, "a preview error must NOT trigger a delete")
	assert.True(t, strings.Contains(err.Error(), "preview"), "error must mention the preview step")
}

// TestHandleOrphanSweep_NilClaimerRefusesDelete is the fail-closed guard for
// finding 1: when the in-flight guard is not wired (nil claimer), the
// destructive dry_run=false path must refuse and delete NOTHING. A guard whose
// only job is to prevent silent data_loss must fail closed (stop the
// destruction), not fail open (proceed without it). The shipped wiring always
// passes deps.Pipeline, so this branch is unreachable in production — which is
// exactly why it must be tested: an unreachable branch is never exercised, so
// if the wiring changes, this is discovered by a test, not by missing rows.
//
// The refusal is a Go error (the handler's existing failure convention — every
// other failure in handleOrphanSweep returns nil, fmt.Errorf(...)), which the
// MCP framework surfaces as IsError=true. It is logged at slog.Error.
//
// Falsifiable on round 1 (a86844ea): the nil-tolerant claimOrSkip returned
// won=true for every candidate, so the handler deleted all of them and
// returned a success result — fake.deletedKeys was non-empty and no error was
// returned.
func TestHandleOrphanSweep_NilClaimerRefusesDelete(t *testing.T) {
	dry := false
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/orphan-a", "test/orphan-b"},
		previewRows: 15076,
		perKeyRows:  map[string]int64{"test/orphan-a": 7000, "test/orphan-b": 8076},
		countResult: 2,
	}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{DryRun: &dry}, fake, nil)
	require.Error(t, err, "nil claimer on the destructive path must refuse, not delete unguarded")
	require.Nil(t, res, "refusal must not return a success result")
	assert.Empty(t, fake.deletedKeys, "nil claimer must delete NOTHING — no per-key deletes")
	assert.Contains(t, err.Error(), "in-flight guard",
		"refusal text must name the missing guard so the operator can see why")
}

// TestHandleOrphanSweep_NilClaimerDryRunStillWorks is the regression guard for
// finding 1's fail-closed fix: a nil claimer must NOT corrupt the dry-run
// preview path. A preview mutates nothing and needs no guard, so it must still
// work and report the candidates. This prevents the over-correction of
// refusing on the dry-run path too.
func TestHandleOrphanSweep_NilClaimerDryRunStillWorks(t *testing.T) {
	fake := &fakeOrphanSweepStore{
		previewKeys: []string{"test/orphan-a", "test/orphan-b"},
		previewRows: 15076,
		perKeyRows:  map[string]int64{"test/orphan-a": 7000, "test/orphan-b": 8076},
	}

	res, err := handleOrphanSweep(context.Background(), OrphanSweepInput{}, fake, nil)
	require.NoError(t, err, "dry run with nil claimer must NOT refuse — a preview mutates nothing")
	require.False(t, res.IsError)

	assert.Equal(t, 1, fake.previewCalls, "dry run must still invoke the preview path")
	assert.Empty(t, fake.deletedKeys, "dry run must NOT delete anything")

	text := textContentOf(t, res)
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "orphan_repo_keys=2")
}
