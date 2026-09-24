package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"

	"github.com/steveyegge/beads/internal/types"
)

// Recognize the deployed HUMAN envelope conservatively, on any canonical host.
// This is a mutation guard, not signature authentication; no HMAC key is needed.
var signedHumanEnvelope = regexp.MustCompile(`(?m)^\[HUMAN [^\]\n]{1,40}? via [^\]\s]+ sig=[0-9a-f]{8}\.[0-9a-f]{16}\] (approved|denied|choice|text|done): .*\r?$`)
var signedHumanReceipt = regexp.MustCompile(`^(answer-consumed|escalated):[0-9a-f]{8}\.[0-9a-f]{16}$`)

// HasSignedHumanAnswer recognizes historical answers, regardless of status,
// labels, or the hostname/environment of the current writer.
func HasSignedHumanAnswer(notes string) bool { return signedHumanEnvelope.MatchString(notes) }

// ValidateSignedHumanUpdate checks the persisted preimage, allowing the first
// answer and byte-identical writes. Comments and non-note fields remain writable.
func ValidateSignedHumanUpdate(old *types.Issue, updates map[string]any) error {
	if !HasSignedHumanAnswer(old.Notes) {
		return nil
	}
	if notes, ok := updates["notes"]; ok && notes != old.Notes {
		return fmt.Errorf("signed HUMAN card %s: notes are immutable; use comments or a linked successor card", old.ID)
	}
	if status, ok := updates["status"]; ok && old.Status == types.StatusClosed && fmt.Sprint(status) != string(types.StatusClosed) {
		return fmt.Errorf("signed HUMAN card %s: cannot reopen a closed card", old.ID)
	}
	return nil
}

// ValidateSignedHumanLabelRemovalInTx protects existing consumption/escalation
// receipts. An absent receipt remains an idempotent removal; adding receipts is
// allowed. The caller supplies the already-routed label table.
func ValidateSignedHumanLabelRemovalInTx(ctx context.Context, tx DBTX, labelTable, id, label string) error {
	if !signedHumanReceipt.MatchString(label) {
		return nil
	}
	issueTable := "issues"
	if labelTable == "wisp_labels" {
		issueTable = "wisps"
	}
	var notes string
	//nolint:gosec // G201: issueTable and labelTable are hardcoded routing constants.
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COALESCE(i.notes, '') FROM %s i JOIN %s l ON l.issue_id = i.id WHERE i.id = ? AND l.label = ?`, issueTable, labelTable), id, label).Scan(&notes)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read signed HUMAN receipt: %w", err)
	}
	if HasSignedHumanAnswer(notes) {
		return fmt.Errorf("signed HUMAN card %s: cannot remove receipt label %s", id, label)
	}
	return nil
}
