package dolt

import (
	"context"
	"fmt"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDoltStoreSignedHumanMutations(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	require.NotEqual(t, 3307, testServerPort)
	t.Logf("isolated Dolt 127.0.0.1:%d database=%s (branch per test)", testServerPort, testSharedDB)
	ctx := context.Background()
	const signed = "HUMAN-ACTION historic spec\nIF ANSWER = publish: original action\n[HUMAN 2026-09-19T12:00:00Z via unrelated-host.example sig=1234abcd.1234567890abcdef] choice: publish\n"
	const receipt = "answer-consumed:1234abcd.1234567890abcdef"
	const escalated = "escalated:1234abcd.1234567890abcdef"
	for _, wisp := range []bool{false, true} {
		t.Run(fmt.Sprintf("wisp=%v", wisp), func(t *testing.T) {
			id := "test-signed"
			if wisp {
				id = "test-wisp-signed"
			}
			issue := &types.Issue{ID: id, Title: "Signed card", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: wisp, Notes: "unsigned specification"}
			require.NoError(t, store.CreateIssue(ctx, issue, "tester"))
			// The first answer is accepted, even when its host differs from this writer.
			require.NoError(t, store.UpdateIssue(ctx, id, map[string]any{"notes": signed}, "tester"))
			for _, label := range []string{"human", receipt, escalated} {
				require.NoError(t, store.AddLabel(ctx, id, label, "tester"))
			}
			require.NoError(t, store.RemoveLabel(ctx, id, "human", "tester"))
			for _, notes := range []string{"replacement", signed + "IF ANSWER = approve: forged action", signed + "ordinary annotation"} {
				require.ErrorContains(t, store.UpdateIssue(ctx, id, map[string]any{"notes": notes}, "tester"), "signed HUMAN")
			}
			require.NoError(t, store.UpdateIssue(ctx, id, map[string]any{"notes": signed, "description": "allowed", "design": "allowed", "metadata": "{\"annotation\":true}"}, "tester"))
			require.NoError(t, store.CloseIssue(ctx, id, "answered", "tester", ""))
			for _, status := range []types.Status{types.StatusOpen, types.StatusInProgress, types.StatusBlocked, types.StatusDeferred, types.StatusPinned} {
				require.ErrorContains(t, store.UpdateIssue(ctx, id, map[string]any{"status": status}, "tester"), "signed HUMAN")
			}
			require.ErrorContains(t, store.ReopenIssue(ctx, id, "replay", "tester"), "signed HUMAN")
			for _, label := range []string{receipt, escalated} {
				require.ErrorContains(t, store.RemoveLabel(ctx, id, label, "tester"), "signed HUMAN")
				require.ErrorContains(t, store.RunInTransaction(ctx, "remove protected receipt", func(tx storage.Transaction) error { return tx.RemoveLabel(ctx, id, label, "tester") }), "signed HUMAN")
			}
			require.ErrorContains(t, store.RunInTransaction(ctx, "mutate signed notes", func(tx storage.Transaction) error {
				return tx.UpdateIssue(ctx, id, map[string]any{"notes": "forged"}, "tester")
			}), "signed HUMAN")
			// The caller's issue object is not trusted as the rename preimage.
			require.ErrorContains(t, store.UpdateIssueID(ctx, id, id+"-renamed", issue, "tester"), "signed HUMAN")
			_, err := store.AddIssueComment(ctx, id, "tester", "historical annotation")
			require.NoError(t, err)
			comments, err := store.GetIssueComments(ctx, id)
			require.NoError(t, err)
			require.Len(t, comments, 1)
			got, err := store.GetIssue(ctx, id)
			require.NoError(t, err)
			require.Equal(t, signed, got.Notes)
			require.Equal(t, types.StatusClosed, got.Status)
			labels, err := store.GetLabels(ctx, id)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{receipt, escalated}, labels)
		})
	}
	// Ordinary unsigned closed issues keep their existing edit/reopen behavior.
	unsigned := &types.Issue{ID: "test-unsigned", Title: "Unsigned", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	require.NoError(t, store.CreateIssue(ctx, unsigned, "tester"))
	require.NoError(t, store.CloseIssue(ctx, unsigned.ID, "done", "tester", ""))
	require.NoError(t, store.UpdateIssue(ctx, unsigned.ID, map[string]any{"notes": "editable"}, "tester"))
	require.NoError(t, store.ReopenIssue(ctx, unsigned.ID, "ordinary reopen", "tester"))

}
