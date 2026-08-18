package main

import (
	"strings"
	"testing"
)

func TestCheckValidationPending(t *testing.T) {
	tests := []struct {
		name    string
		labels  []string
		wantErr bool
	}{
		{
			name:    "no labels",
			labels:  nil,
			wantErr: false,
		},
		{
			name:    "unrelated labels",
			labels:  []string{"bug", "p1", "area:storage"},
			wantErr: false,
		},
		{
			name:    "validation pending",
			labels:  []string{"validation:pending"},
			wantErr: true,
		},
		{
			name:    "validation pending among others",
			labels:  []string{"bug", "validation:pending", "area:storage"},
			wantErr: true,
		},
		{
			name:    "validation proven",
			labels:  []string{"validation:proven"},
			wantErr: false,
		},
		{
			name:    "other validation value",
			labels:  []string{"validation:waived"},
			wantErr: false,
		},
		{
			name:    "different dimension named pending",
			labels:  []string{"patrol:pending"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkValidationPending("tc-1", tt.labels)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				// The message has to name the escape hatch and the documented
				// way out, or an agent hitting it just reaches for --force.
				msg := err.Error()
				for _, want := range []string{"tc-1", "bd set-state tc-1 validation=proven", "--force"} {
					if !strings.Contains(msg, want) {
						t.Errorf("error message %q missing %q", msg, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}
