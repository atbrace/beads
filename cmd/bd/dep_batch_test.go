package main

import (
	"context"
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

func TestDepListAcceptsMultipleArgs(t *testing.T) {
	// Verify the cobra command accepts multiple args (MinimumNArgs(1)).
	cmd := depListCmd
	if err := cmd.Args(cmd, []string{"id1", "id2", "id3"}); err != nil {
		t.Fatalf("depListCmd should accept multiple args: %v", err)
	}
}

func TestDepListRequiresAtLeastOneArg(t *testing.T) {
	cmd := depListCmd
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Fatal("depListCmd should reject zero args")
	}
}

func TestDepListAcceptsSingleArg(t *testing.T) {
	cmd := depListCmd
	if err := cmd.Args(cmd, []string{"id1"}); err != nil {
		t.Fatalf("depListCmd should accept single arg: %v", err)
	}
}

// bulkProbeStore counts every call the batch resolution could make, so the
// tests below pin the MECHANISM — one bulk read — and not just the answer.
// Any per-id GetIssue or SearchIssues call is the pre-fix behavior returning:
// two queries per argument, ~60ms each on dolt, and a 780-id batch that
// measures 44-70s where one bulk read measures milliseconds.
type bulkProbeStore struct {
	storage.DoltStorage
	issues   map[string]*types.Issue
	err      error // forces the bulk read to fail
	bulkReqs [][]string
	perID    int
}

func (s *bulkProbeStore) GetIssuesByIDs(_ context.Context, ids []string) ([]*types.Issue, error) {
	s.bulkReqs = append(s.bulkReqs, append([]string(nil), ids...))
	if s.err != nil {
		return nil, s.err
	}
	var out []*types.Issue
	for _, id := range ids {
		if issue, ok := s.issues[id]; ok {
			out = append(out, issue)
		}
	}
	return out, nil
}

func (s *bulkProbeStore) GetIssue(_ context.Context, _ string) (*types.Issue, error) {
	s.perID++
	return nil, errors.New("no issue found")
}

func (s *bulkProbeStore) SearchIssues(_ context.Context, _ string, _ types.IssueFilter) ([]*types.Issue, error) {
	s.perID++
	return nil, nil
}

func TestDepListBatchResolvesExactIDsInOneBulkRead(t *testing.T) {
	store := &bulkProbeStore{issues: map[string]*types.Issue{
		"bd-1": {ID: "bd-1"},
		"bd-2": {ID: "bd-2"},
		"bd-3": {ID: "bd-3"},
	}}
	// bd-missing exists nowhere and bd-1 is repeated: the bulk read must see
	// each distinct arg once, and only exact hits may become anchors.
	anchors := bulkLocalDepListAnchors(context.Background(), store,
		[]string{"bd-1", "bd-2", "bd-missing", "bd-1", "bd-3"})

	if len(store.bulkReqs) != 1 {
		t.Fatalf("bulk reads = %d, want exactly 1", len(store.bulkReqs))
	}
	want := []string{"bd-1", "bd-2", "bd-missing", "bd-3"}
	if len(store.bulkReqs[0]) != len(want) {
		t.Fatalf("bulk ids = %v, want %v (duplicates collapsed)", store.bulkReqs[0], want)
	}
	if len(anchors) != 3 {
		t.Fatalf("anchors = %d, want 3 (the missing id must fall through to routing)", len(anchors))
	}
	if _, ok := anchors["bd-missing"]; ok {
		t.Fatal("bd-missing must not anchor from the bulk answer")
	}
	for id, anchor := range anchors {
		if anchor.fullID != id || anchor.store != storage.DoltStorage(store) {
			t.Fatalf("anchor %q = %+v, want fullID %q on the local store", id, anchor, id)
		}
	}
	if store.perID != 0 {
		t.Fatalf("per-id lookups = %d, want 0: two queries per argument is the regression", store.perID)
	}
}

func TestDepListSingleArgStaysOnRoutingPath(t *testing.T) {
	// The fix is scoped to the batch path; a single id must not pay (or
	// change behavior on) the bulk read.
	store := &bulkProbeStore{issues: map[string]*types.Issue{"bd-1": {ID: "bd-1"}}}
	if anchors := bulkLocalDepListAnchors(context.Background(), store, []string{"bd-1"}); anchors != nil {
		t.Fatalf("single arg returned %+v, want nil (no bulk path)", anchors)
	}
	if len(store.bulkReqs) != 0 {
		t.Fatalf("bulk reads = %d, want 0 for a single arg", len(store.bulkReqs))
	}
}

func TestDepListBatchFallsBackWhenBulkReadFails(t *testing.T) {
	// A failing bulk read degrades to per-id resolution — the shape of
	// today's command — and never becomes a new failure mode.
	store := &bulkProbeStore{err: errors.New("database is locked")}
	if anchors := bulkLocalDepListAnchors(context.Background(), store, []string{"bd-1", "bd-2"}); anchors != nil {
		t.Fatalf("bulk failure returned %+v, want nil so the per-argument loop answers", anchors)
	}
}
