//go:build cgo

package main

import "testing"

// TestBuildListFilterMoleculeSearchesWispsTier pins that `bd list -t molecule`
// searches the wisps tier. Molecules are stored there, but "molecule" is not a
// member of the infra set (agent/role/message), so the SkipWisps guard fired on
// the very type whose rows only exist in the tier it skips — making the query
// structurally incapable of matching a row.
//
// Observed as a wisp leak: mol-witness-patrol reconciles its open patrol wisps
// with `bd list --assignee=$GC_AGENT --status=open --type=molecule`, always got
// back an empty set, never found the prior wisp to burn, and poured a fresh one
// every cycle.
func TestBuildListFilterMoleculeSearchesWispsTier(t *testing.T) {
	cfg := listFilterConfig{}

	got, err := buildListFilter(listInput{issueType: "molecule", status: "open"}, cfg)
	if err != nil {
		t.Fatalf("buildListFilter: %v", err)
	}
	if got.SkipWisps {
		t.Error("SkipWisps = true for -t molecule; molecules live in the wisps tier, so the query can never match a row")
	}
}

// TestBuildListFilterSkipWispsDefaults guards the other direction: the wisps
// merge stays off for the durable types, so fixing the molecule case does not
// silently widen every other listing.
func TestBuildListFilterSkipWispsDefaults(t *testing.T) {
	cfg := listFilterConfig{}

	for _, tc := range []struct {
		issueType    string
		includeInfra bool
		wantSkip     bool
	}{
		{issueType: "", wantSkip: true},                          // no type filter: durable tier only
		{issueType: "task", wantSkip: true},                      // durable type
		{issueType: "bug", wantSkip: true},                       // durable type
		{issueType: "message", wantSkip: false},                  // infra type: already routed to wisps
		{issueType: "", includeInfra: true, wantSkip: false},     // explicit opt-in
		{issueType: "task", includeInfra: true, wantSkip: false}, // explicit opt-in
	} {
		name := tc.issueType
		if name == "" {
			name = "none"
		}
		if tc.includeInfra {
			name += "_include_infra"
		}
		t.Run("type_"+name, func(t *testing.T) {
			got, err := buildListFilter(listInput{issueType: tc.issueType, includeInfra: tc.includeInfra}, cfg)
			if err != nil {
				t.Fatalf("buildListFilter: %v", err)
			}
			if got.SkipWisps != tc.wantSkip {
				t.Errorf("SkipWisps = %v, want %v", got.SkipWisps, tc.wantSkip)
			}
		})
	}
}
