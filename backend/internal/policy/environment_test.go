package policy

import (
	"reflect"
	"testing"
)

func TestDecodeEnvironmentRule(t *testing.T) {
	t.Run("empty rules decode to the zero rule", func(t *testing.T) {
		got, err := DecodeEnvironmentRule(nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, EnvironmentRule{}) {
			t.Fatalf("want zero rule, got %+v", got)
		}
	})

	t.Run("round-trips through storage encoding", func(t *testing.T) {
		in := EnvironmentRule{
			RequiredApprovals:          2,
			AllowedSourceBranches:      []string{"main", "release/*"},
			RequirePreviousEnvironment: "staging",
			RequireSuccessfulRun:       true,
			MinWaitMinutes:             30,
		}
		raw, err := in.MarshalRules()
		if err != nil {
			t.Fatal(err)
		}
		out, err := DecodeEnvironmentRule(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(out, in) {
			t.Fatalf("round-trip mismatch: %+v != %+v", out, in)
		}
	})
}

func TestEnvironmentApplies(t *testing.T) {
	p := ScopedPolicy{Scope: ScopeProject, Selector: "prod-*"}
	cases := map[string]bool{
		"prod-eu": true,
		"prod-us": true,
		"staging": false,
	}
	for env, want := range cases {
		facts := Context{Environment: &EnvironmentFacts{Environment: env}}
		if got := environmentApplies(p, facts); got != want {
			t.Fatalf("environmentApplies(%q)=%v want %v", env, got, want)
		}
	}
	if environmentApplies(p, Context{}) {
		t.Fatal("expected no match when environment facts are absent")
	}
}
