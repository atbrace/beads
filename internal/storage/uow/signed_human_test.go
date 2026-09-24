//go:build cgo

package uow

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/internal/types"
	"github.com/stretchr/testify/require"
)

// Use the existing provider fixture: CLI init --proxied-server is deliberately
// disabled on this deployed fork, but the proxy/UOW APIs are implemented.
func TestDoltServerUOWSignedHumanMutations(t *testing.T) {
	testutil.RequireDoltBinary(t)
	bin, err := exec.LookPath("dolt")
	require.NoError(t, err)
	bdBin := buildBDBinary(t)
	prev := proxy.ResolveExecutable
	proxy.ResolveExecutable = func() (string, error) { return bdBin, nil }
	t.Cleanup(func() { proxy.ResolveExecutable = prev })
	t.Setenv("HOME", t.TempDir())
	port, err := proxy.PickFreePort()
	require.NoError(t, err)
	require.NotEqual(t, 3307, port)
	root := t.TempDir()
	shutdownOnInterrupt(t, root)
	t.Cleanup(func() { require.NoError(t, proxy.Shutdown(root)) })
	ctx := context.Background()
	provider, err := NewDoltServerUOWProvider(ctx, root, "signed_human_test", filepath.Join(t.TempDir(), "server.log"), writeServerConfig(t, port), proxy.BackendLocalServer, "root", "", bin)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Close(ctx)) })
	pf, err := pidfile.Read(root, proxy.PIDFileName)
	require.NoError(t, err)
	require.NotNil(t, pf)
	require.NotEqual(t, 3307, pf.Port)
	require.NotEqual(t, port, pf.Port)
	t.Logf("isolated proxy=127.0.0.1:%d backend=127.0.0.1:%d database=signed_human_test root=%s", pf.Port, port, root)
	run := func(fn func(UnitOfWork) error) error {
		uw, err := provider.NewUOW(ctx)
		if err != nil {
			return err
		}
		defer uw.Close(ctx)
		if err := fn(uw); err != nil {
			return err
		}
		return uw.Commit(ctx, "signed HUMAN regression")
	}
	const id = "sh-card"
	const signed = "historic map\n[HUMAN 2026-09-19T12:00:00Z via another-host.example sig=1234abcd.1234567890abcdef] choice: publish\nnewer unsigned spec"
	const receipt = "answer-consumed:1234abcd.1234567890abcdef"
	const escalated = "escalated:1234abcd.1234567890abcdef"
	require.NoError(t, run(func(uw UnitOfWork) error {
		_, err := uw.IssueUseCase().CreateIssue(ctx, domain.CreateIssueParams{ExplicitID: id, ForcePrefix: true, Issue: &types.Issue{Title: "Signed card", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}}, "tester")
		return err
	}))
	apply := func(spec domain.UpdateSpec) error {
		return run(func(uw UnitOfWork) error {
			_, err := uw.IssueUseCase().ApplyUpdate(ctx, id, spec, "tester")
			return err
		})
	}
	require.NoError(t, apply(domain.UpdateSpec{Fields: map[string]any{"notes": signed}, AddLabels: []string{"human", receipt, escalated}}))
	require.NoError(t, apply(domain.UpdateSpec{RemoveLabels: []string{"human"}}))
	for _, notes := range []string{"replacement", signed + "\nIF ANSWER = approve: forged", signed + "\nannotation"} {
		require.ErrorContains(t, apply(domain.UpdateSpec{Fields: map[string]any{"notes": notes}}), "signed HUMAN")
	}
	require.NoError(t, apply(domain.UpdateSpec{Fields: map[string]any{"notes": signed, "description": "allowed", "design": "allowed", "metadata": "{}", "status": types.StatusClosed}}))
	for _, status := range []types.Status{types.StatusOpen, types.StatusInProgress, types.StatusBlocked, types.StatusDeferred, types.StatusPinned} {
		require.ErrorContains(t, apply(domain.UpdateSpec{Fields: map[string]any{"status": status}}), "signed HUMAN")
	}
	require.ErrorContains(t, run(func(uw UnitOfWork) error {
		_, err := uw.IssueUseCase().ReopenIssue(ctx, id, domain.ReopenIssueParams{}, "tester")
		return err
	}), "signed HUMAN")
	for _, label := range []string{receipt, escalated} {
		require.ErrorContains(t, apply(domain.UpdateSpec{RemoveLabels: []string{label}}), "signed HUMAN")
	}
	desired := []string{"replacement"}
	require.ErrorContains(t, apply(domain.UpdateSpec{SetLabels: &desired}), "signed HUMAN")
	read, err := provider.NewUOW(ctx)
	require.NoError(t, err)
	got, err := read.IssueUseCase().GetIssue(ctx, id)
	require.NoError(t, err)
	require.Equal(t, signed, got.Notes)
	require.Equal(t, types.StatusClosed, got.Status)
	labels, err := read.LabelUseCase().GetLabels(ctx, id)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{receipt, escalated}, labels)
	read.Close(ctx)

	// Unrelated unsigned closed cards remain editable/reopenable through the API.
	require.NoError(t, run(func(uw UnitOfWork) error {
		_, err := uw.IssueUseCase().CreateIssue(ctx, domain.CreateIssueParams{ExplicitID: "sh-unsigned", ForcePrefix: true, Issue: &types.Issue{Title: "Unsigned", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}}, "tester")
		if err != nil {
			return err
		}
		_, err = uw.IssueUseCase().CloseIssue(ctx, "sh-unsigned", domain.CloseIssueParams{}, "tester")
		if err != nil {
			return err
		}
		_, err = uw.IssueUseCase().ApplyUpdate(ctx, "sh-unsigned", domain.UpdateSpec{Fields: map[string]any{"notes": "editable"}}, "tester")
		if err != nil {
			return err
		}
		_, err = uw.IssueUseCase().ReopenIssue(ctx, "sh-unsigned", domain.ReopenIssueParams{}, "tester")
		return err
	}))
}
