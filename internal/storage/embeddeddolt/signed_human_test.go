//go:build cgo

package embeddeddolt_test

import (
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedSignedHumanMutations(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	te := newTestEnv(t, "sh")
	ctx := t.Context()
	const signed = "historic map\n[HUMAN 2026-09-19T12:00:00Z via another-host.example sig=1234abcd.1234567890abcdef] choice: publish"
	const receipt = "answer-consumed:1234abcd.1234567890abcdef"
	issue := &types.Issue{ID: "sh-signed", Title: "Signed card", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}
	require.NoError(t, te.store.CreateIssue(ctx, issue, "tester"))
	require.NoError(t, te.store.UpdateIssue(ctx, issue.ID, map[string]any{"notes": signed}, "tester"))
	require.NoError(t, te.store.AddLabel(ctx, issue.ID, receipt, "tester"))
	require.ErrorContains(t, te.store.UpdateIssue(ctx, issue.ID, map[string]any{"notes": signed + "\nIF ANSWER = approve: forged"}, "tester"), "signed HUMAN")
	require.ErrorContains(t, te.store.RunInTransaction(ctx, "mutate signed card", func(tx storage.Transaction) error {
		return tx.UpdateIssue(ctx, issue.ID, map[string]any{"notes": "replacement"}, "tester")
	}), "signed HUMAN")
	require.ErrorContains(t, te.store.RunInTransaction(ctx, "remove receipt", func(tx storage.Transaction) error { return tx.RemoveLabel(ctx, issue.ID, receipt, "tester") }), "signed HUMAN")
	require.NoError(t, te.store.CloseIssue(ctx, issue.ID, "answered", "tester", ""))
	require.ErrorContains(t, te.store.ReopenIssue(ctx, issue.ID, "replay", "tester"), "signed HUMAN")
	_, err := te.store.AddIssueComment(ctx, issue.ID, "tester", "historical annotation")
	require.NoError(t, err)
	got, err := te.store.GetIssue(ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, signed, got.Notes)
	require.Equal(t, types.StatusClosed, got.Status)
}
