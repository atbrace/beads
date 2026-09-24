//go:build cgo

package tracker

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEnginePullPreservesSignedHumanCard(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	require.NotEqual(t, 3307, testServerPort)
	t.Logf("isolated Dolt 127.0.0.1:%d database=%s (branch per test)", testServerPort, testSharedDB)
	const signed = "historic map\n[HUMAN 2026-09-19T12:00:00Z via another-host.example sig=1234abcd.1234567890abcdef] choice: publish"
	const receipt = "answer-consumed:1234abcd.1234567890abcdef"
	issue := &types.Issue{ID: "bd-signed-pull", Title: "Original", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Notes: signed}
	require.NoError(t, store.CreateIssue(ctx, issue, "tester"))
	require.NoError(t, store.AddLabel(ctx, issue.ID, receipt, "tester"))
	tracker := newMockTracker("test")
	tracker.issues = []TrackerIssue{{ID: "EXT-SIGNED", Identifier: "EXT-SIGNED", Title: "Remote", UpdatedAt: time.Now().UTC()}}
	remoteStatus := types.StatusOpen
	tracker.fieldMapper = &mockMapper{issueToBeads: func(ti *TrackerIssue) *IssueConversion {
		return &IssueConversion{Issue: &types.Issue{ID: issue.ID, Title: ti.Title, Priority: 2, Status: remoteStatus, IssueType: types.TypeTask}}
	}}
	engine := NewEngine(tracker, store, "tester")
	// Pull's bulk transaction must roll back a title update when label replacement
	// would remove an existing receipt.
	result, err := engine.Sync(ctx, SyncOptions{Pull: true})
	require.NoError(t, err)
	require.Zero(t, result.PullStats.Updated)
	got, err := store.GetIssue(ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, "Original", got.Title)
	require.Equal(t, signed, got.Notes)
	labels, err := store.GetLabels(ctx, issue.ID)
	require.NoError(t, err)
	require.Contains(t, labels, receipt)
	require.NoError(t, store.CloseIssue(ctx, issue.ID, "answered", "tester", ""))
	result, err = engine.Sync(ctx, SyncOptions{Pull: true})
	require.NoError(t, err)
	require.Zero(t, result.PullStats.Updated)
	got, err = store.GetIssue(ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, types.StatusClosed, got.Status)
}
