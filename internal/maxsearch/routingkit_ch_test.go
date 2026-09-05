package maxsearch

import (
	"testing"
)

func TestParseRoutingKitCHReachable(t *testing.T) {
	got, err := parseRoutingKitCHResponse("R 7 1234 3 0 1 2\n", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stats.Reachable || got.Stats.Distance != 7 || got.Stats.DurationNS != 1234 {
		t.Fatalf("stats=%+v", got.Stats)
	}
	if len(got.Path) != 3 || got.Path[0] != 0 || got.Path[2] != 2 {
		t.Fatalf("path=%v", got.Path)
	}
}

func TestParseRoutingKitCHUnreachable(t *testing.T) {
	got, err := parseRoutingKitCHResponse("U 99\n", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Reachable || got.Stats.DurationNS != 99 || len(got.Path) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseRoutingKitCHRejectsBadPath(t *testing.T) {
	for _, response := range []string{
		"R 7 10 2 0\n",
		"R 7 10 1 99\n",
		"U 0\n",
		"E invalid-path\n",
	} {
		if _, err := parseRoutingKitCHResponse(response, 3); err == nil {
			t.Fatalf("accepted %q", response)
		}
	}
}
