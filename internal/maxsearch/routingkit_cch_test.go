package maxsearch

import "testing"

func TestParseRoutingKitCCHReachable(t *testing.T) {
	got, err := parseRoutingKitCCHResponse("R 11 777 4 0 1 2 3\n", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stats.Reachable || got.Stats.Distance != 11 || got.Stats.DurationNS != 777 {
		t.Fatalf("stats=%+v", got.Stats)
	}
	if got.Stats.Algorithm != RoutingKitCCH {
		t.Fatalf("algorithm=%q", got.Stats.Algorithm)
	}
	if len(got.Path) != 4 || got.Path[0] != 0 || got.Path[3] != 3 {
		t.Fatalf("path=%v", got.Path)
	}
}

func TestParseRoutingKitCCHUnreachable(t *testing.T) {
	got, err := parseRoutingKitCCHResponse("U 55\n", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Reachable || got.Stats.DurationNS != 55 || got.Stats.Algorithm != RoutingKitCCH {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseRoutingKitCCHRejectsBadResponses(t *testing.T) {
	for _, response := range []string{
		"R 7 10 2 0\n",
		"R 7 10 1 99\n",
		"U 0\n",
		"READY 1 2 3 fingerprint\n",
		"E invalid-path\n",
	} {
		if _, err := parseRoutingKitCCHResponse(response, 3); err == nil {
			t.Fatalf("accepted %q", response)
		}
	}
}

func TestParsePositiveInt64(t *testing.T) {
	for _, good := range []string{"1", "999999"} {
		if _, err := parsePositiveInt64(good); err != nil {
			t.Fatalf("rejected %q: %v", good, err)
		}
	}
	for _, bad := range []string{"0", "-1", "x"} {
		if _, err := parsePositiveInt64(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
