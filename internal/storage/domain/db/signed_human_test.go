package db

import (
	"fmt"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/internal/types"
)

func (s *testSuite) TestSignedHumanMutations() {
	s.NotEqual(3307, testutil.DoltContainerPortInt())
	s.T().Logf("isolated Dolt 127.0.0.1:%d database=%s", testutil.DoltContainerPortInt(), s.dbName)
	const signed = "historic map\n[HUMAN 2026-09-19T12:00:00Z via another-host.example sig=1234abcd.1234567890abcdef] choice: publish\nnewer unsigned spec"
	const receipt = "answer-consumed:1234abcd.1234567890abcdef"
	const escalated = "escalated:1234abcd.1234567890abcdef"
	for _, wisp := range []bool{false, true} {
		s.Run(fmt.Sprintf("wisp=%v", wisp), func() {
			id := "bd-signed"
			if wisp {
				id = "bd-wisp-signed"
			}
			issue := newTestIssue(id, "Signed card")
			issue.Ephemeral = wisp
			opts := domain.IssueTableOpts{UseWispsTable: wisp}
			lopts := domain.LabelOpts{UseWispsTable: wisp}
			repo := s.issueRepo()
			labels := NewLabelSQLRepository(s.Runner())
			uc := s.issueUseCase()
			s.Require().NoError(repo.Insert(s.Ctx(), issue, "tester", domain.InsertIssueOpts{UseWispsTable: wisp}))
			_, err := uc.ApplyUpdate(s.Ctx(), id, domain.UpdateSpec{Fields: map[string]any{"notes": signed}}, "tester")
			s.Require().NoError(err)
			for _, label := range []string{"human", receipt, escalated} {
				s.Require().NoError(labels.Insert(s.Ctx(), id, label, "tester", lopts))
			}
			s.Require().NoError(labels.Delete(s.Ctx(), id, "human", "tester", lopts))
			for _, notes := range []string{"replacement", signed + "\nIF ANSWER = approve: forged", signed + "\nannotation"} {
				s.ErrorContains(repo.Update(s.Ctx(), id, map[string]any{"notes": notes}, "tester", opts), "signed HUMAN")
				_, err := uc.ApplyUpdate(s.Ctx(), id, domain.UpdateSpec{Fields: map[string]any{"notes": notes}}, "tester")
				s.ErrorContains(err, "signed HUMAN")
			}
			s.Require().NoError(repo.Update(s.Ctx(), id, map[string]any{"notes": signed, "description": "allowed", "design": "allowed", "metadata": "{}", "status": types.StatusClosed}, "tester", opts))
			for _, status := range []types.Status{types.StatusOpen, types.StatusInProgress, types.StatusBlocked, types.StatusDeferred, types.StatusPinned} {
				_, err := uc.ApplyUpdate(s.Ctx(), id, domain.UpdateSpec{Fields: map[string]any{"status": status}}, "tester")
				s.ErrorContains(err, "signed HUMAN")
			}
			_, err = repo.Reopen(s.Ctx(), id, domain.ReopenRowParams{}, "tester", opts)
			s.ErrorContains(err, "signed HUMAN")
			for _, label := range []string{receipt, escalated} {
				s.ErrorContains(labels.Delete(s.Ctx(), id, label, "tester", lopts), "signed HUMAN")
			}
			replacement := []string{"answer-consumed:ffffffff.ffffffffffffffff"}
			_, err = uc.ApplyUpdate(s.Ctx(), id, domain.UpdateSpec{SetLabels: &replacement}, "tester")
			s.ErrorContains(err, "signed HUMAN")
			_, err = labels.DeleteAllForIDs(s.Ctx(), []string{id}, lopts)
			s.ErrorContains(err, "signed HUMAN")
			got, err := repo.Get(s.Ctx(), id, opts)
			s.Require().NoError(err)
			s.Equal(signed, got.Notes)
			s.Equal(types.StatusClosed, got.Status)
			kept, err := labels.List(s.Ctx(), id, lopts)
			s.Require().NoError(err)
			s.ElementsMatch([]string{receipt, escalated}, kept)
		})
	}
}
