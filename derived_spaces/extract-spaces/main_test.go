package main

import (
	"reflect"
	"testing"
)

// TestScanTenantsGolden is the parser <-> component-stack module parity
// anchor: every wiring style the fixtures use (literal, local, var+tfvars,
// concat, jsondecode/file ownership lookup) must resolve to exactly these
// spaces. The same derivation lives in HCL inside
// derived_spaces/modules/component-stack/main.tf — if you change one,
// change both and update this test.
func TestScanTenantsGolden(t *testing.T) {
	calls, errs := ScanTenants("testdata/tenants", "modules/component-stack", "team/")
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	wantCalls := map[string]string{
		"svc_payments_dev":      "custom-teama-teamb",
		"svc_alpha_dev":         "teama",
		"svc_data_platform_dev": "custom-teama-teamb-teamc",
		"svc_search_dev":        "teama",
	}
	if len(calls) != len(wantCalls) {
		t.Fatalf("got %d calls, want %d: %+v", len(calls), len(wantCalls), calls)
	}
	for _, c := range calls {
		if want := wantCalls[c.Module]; c.Space != want {
			t.Errorf("module %s: space = %q, want %q", c.Module, c.Space, want)
		}
	}

	out := Aggregate(calls)
	wantSpaces := map[string][]string{
		"teama":                    {"teama"},
		"custom-teama-teamb":       {"teama", "teamb"},
		"custom-teama-teamb-teamc": {"teama", "teamb", "teamc"},
	}
	if len(out.Spaces) != len(wantSpaces) {
		t.Fatalf("got %d spaces, want %d: %+v", len(out.Spaces), len(wantSpaces), out.Spaces)
	}
	for name, teams := range wantSpaces {
		spec, ok := out.Spaces[name]
		if !ok {
			t.Errorf("missing space %q", name)
			continue
		}
		if !reflect.DeepEqual(spec.Teams, teams) {
			t.Errorf("space %s: teams = %v, want %v", name, spec.Teams, teams)
		}
	}

	// The shared single-team space must record both tenants (dedupe proof).
	if got := out.Spaces["teama"].Tenants; !reflect.DeepEqual(got, []string{"fixture-a", "fixture-b"}) {
		t.Errorf("space teama: tenants = %v, want [fixture-a fixture-b]", got)
	}
}

// TestUnresolvedIsFatal pins the loud-failure contract: a labels expression
// that cannot be resolved statically produces an error, never a silent skip.
func TestUnresolvedIsFatal(t *testing.T) {
	calls, errs := ScanTenants("testdata/unresolved", "modules/component-stack", "team/")
	if len(calls) != 0 {
		t.Fatalf("expected no resolved calls, got %+v", calls)
	}
	if len(errs) != 1 {
		t.Fatalf("expected exactly one error, got %v", errs)
	}
}

func TestSpaceName(t *testing.T) {
	cases := []struct {
		teams []string
		want  string
	}{
		{[]string{"teama"}, "teama"},
		{[]string{"teama", "teamb"}, "custom-teama-teamb"},
		{[]string{"teama", "teamb", "teamc"}, "custom-teama-teamb-teamc"},
	}
	for _, c := range cases {
		if got := SpaceName(c.teams); got != c.want {
			t.Errorf("SpaceName(%v) = %q, want %q", c.teams, got, c.want)
		}
	}
}
