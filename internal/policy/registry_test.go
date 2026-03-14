package policy

import "testing"

func TestResolveReturnsDefaultForUnknownPolicy(t *testing.T) {
	if got := Resolve("missing-policy").Name(); got != Default().Name() {
		t.Fatalf("Resolve(unknown)=%q want %q", got, Default().Name())
	}
}

func TestAvailableIncludesRequestedBuiltins(t *testing.T) {
	want := map[string]bool{
		"default":                 false,
		"poc-completion":          false,
		"aggressive-architecture": false,
		"parity-porting":          false,
		"creative-exploration":    false,
	}

	for _, policy := range Available() {
		if _, ok := want[policy.Name()]; ok {
			want[policy.Name()] = true
		}
	}

	for name, seen := range want {
		if !seen {
			t.Fatalf("policy %q not found in Available()", name)
		}
	}
}

func TestContinuationSpecReturnsCopies(t *testing.T) {
	spec := Default().Continuation()
	spec.BasePrompts[0] = "mutated"

	fresh := Default().Continuation()
	if fresh.BasePrompts[0] == "mutated" {
		t.Fatal("Continuation() should return defensive copies")
	}
}
