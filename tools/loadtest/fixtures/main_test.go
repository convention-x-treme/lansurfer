package main

import (
	"reflect"
	"testing"
)

func ids(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + string(rune('a'+i%26)) + string(rune('A'+i/26))
	}
	return out
}

func TestPlanRequestsInvariants(t *testing.T) {
	users, games := ids("u", 400), ids("g", 10)
	plan := planRequests(users, games, 60, 1)
	if len(plan) != 60 {
		t.Fatalf("planned %d requests", len(plan))
	}
	kinds := map[string]int{}
	for i, p := range plan {
		kinds[p.Kind]++
		seen := map[string]bool{p.Creator: true}
		for _, m := range p.Joiners {
			if seen[m] {
				t.Fatalf("request %d: duplicate member %s", i, m)
			}
			seen[m] = true
		}
		if members := len(p.Joiners) + 1; members > int(p.MaxPlayers) {
			t.Fatalf("request %d: %d members exceeds max %d", i, members, p.MaxPlayers)
		}
		if p.Kind == "nearly_full" && len(p.Joiners)+1 != int(p.MaxPlayers)-1 {
			t.Fatalf("request %d: nearly_full must leave exactly one place", i)
		}
		if p.Kind == "empty" && len(p.Joiners) != 0 {
			t.Fatalf("request %d: empty has joiners", i)
		}
	}
	for _, k := range []string{"open", "nearly_full", "playing", "empty"} {
		if kinds[k] == 0 {
			t.Fatalf("no %s requests planned: %v", k, kinds)
		}
	}
}

func TestPlanRequestsDeterministic(t *testing.T) {
	users, games := ids("u", 50), ids("g", 5)
	if !reflect.DeepEqual(planRequests(users, games, 30, 7), planRequests(users, games, 30, 7)) {
		t.Fatal("same seed produced different plans")
	}
}
