package issueops

import (
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestSignedHumanEnvelope(t *testing.T) {
	line := "[HUMAN 2026-09-19T12:00:00Z via beadash.remote.example sig=1234abcd.1234567890abcdef] choice: publish"
	for _, kind := range []string{"approved", "denied", "choice", "text", "done"} {
		if !HasSignedHumanAnswer("historic spec\n" + strings.Replace(line, "choice:", kind+":", 1) + "\nnewer unsigned spec") {
			t.Errorf("missed historical %s answer", kind)
		}
	}
	for _, notes := range []string{
		"prefix " + line, strings.Replace(line, "1234abcd.", "1234abc.", 1),
		strings.Replace(line, "1234567890abcdef", "1234567890abcdeg", 1),
		strings.Replace(line, "choice:", "unknown:", 1), "[HUMAN unsigned] choice: publish",
	} {
		if HasSignedHumanAnswer(notes) {
			t.Errorf("recognized malformed envelope %q", notes)
		}
	}
	old := &types.Issue{ID: "test-card", Notes: line, Status: types.StatusClosed}
	for _, updates := range []map[string]any{{"notes": line}, {"status": types.StatusClosed}, {"metadata": "{}", "description": "annotation", "design": "revision"}} {
		if err := ValidateSignedHumanUpdate(old, updates); err != nil {
			t.Fatal(err)
		}
	}
}
