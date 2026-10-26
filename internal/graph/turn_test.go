package graph

import (
	"os"
	"strings"
	"testing"
)

// The two restrictions below are hand-written OSM relations in the exact shape
// produced by an Overpass/api extract: a no_left_turn (no semantics, via node)
// and an only_straight_on (only semantics, via node).
const turnRestrictionXML = `<?xml version="1.0" encoding="UTF-8"?>
<osm version="0.6">
  <node id="100" lat="35.6812" lon="139.7671"/>
  <node id="101" lat="35.6813" lon="139.7672"/>
  <node id="102" lat="35.6814" lon="139.7673"/>
  <node id="103" lat="35.6815" lon="139.7674"/>
  <way id="900">
    <nd ref="100"/>
    <nd ref="101"/>
    <tag k="highway" v="primary"/>
    <tag k="name" v="From Road"/>
  </way>
  <way id="901">
    <nd ref="101"/>
    <nd ref="102"/>
    <tag k="highway" v="secondary"/>
    <tag k="name" v="To Road"/>
  </way>
  <way id="902">
    <nd ref="101"/>
    <nd ref="103"/>
    <tag k="highway" v="residential"/>
  </way>
  <relation id="5001">
    <member type="way" ref="900" role="from"/>
    <member type="node" ref="101" role="via"/>
    <member type="way" ref="901" role="to"/>
    <tag k="type" v="restriction"/>
    <tag k="restriction" v="no_left_turn"/>
  </relation>
  <relation id="5002">
    <member type="way" ref="900" role="from"/>
    <member type="node" ref="101" role="via"/>
    <member type="way" ref="902" role="to"/>
    <tag k="type" v="restriction"/>
    <tag k="restriction" v="only_straight_on"/>
    <tag k="except" v="bicycle;psv"/>
  </relation>
</osm>`

// TestParseTurnRestrictionsTwoRestrictions is the acceptance test for the
// prototype: small hand-written OSM XML with two restrictions parses into two
// fully-populated TurnRestriction values.
func TestParseTurnRestrictionsTwoRestrictions(t *testing.T) {
	rs, err := ParseTurnRestrictions(strings.NewReader(turnRestrictionXML))
	if err != nil {
		t.Fatalf("ParseTurnRestrictions: %v", err)
	}
	if len(rs) != 2 {
		t.Fatalf("got %d restrictions, want 2: %+v", len(rs), rs)
	}

	no := rs[0]
	if no.RelationID != 5001 {
		t.Errorf("RelationID = %d, want 5001", no.RelationID)
	}
	if no.Value != "no_left_turn" {
		t.Errorf("Value = %q, want no_left_turn", no.Value)
	}
	if no.Kind != NoTurn {
		t.Errorf("Kind = %v, want NoTurn", no.Kind)
	}
	if no.FromWay != 900 || no.ToWay != 901 {
		t.Errorf("FromWay/ToWay = %d/%d, want 900/901", no.FromWay, no.ToWay)
	}
	if no.ViaNode != 101 {
		t.Errorf("ViaNode = %d, want 101", no.ViaNode)
	}
	if no.ViaIsWay() {
		t.Errorf("no_left_turn unexpectedly has via ways: %v", no.ViaWays)
	}
	if !no.AppliesTo("car") {
		t.Error("no_left_turn should apply to car")
	}

	only := rs[1]
	if only.RelationID != 5002 {
		t.Errorf("RelationID = %d, want 5002", only.RelationID)
	}
	if only.Value != "only_straight_on" || only.Kind != OnlyTurn {
		t.Errorf("Value/Kind = %q/%v, want only_straight_on/OnlyTurn", only.Value, only.Kind)
	}
	if only.FromWay != 900 || only.ToWay != 902 || only.ViaNode != 101 {
		t.Errorf("from/to/via = %d/%d/%d, want 900/902/101", only.FromWay, only.ToWay, only.ViaNode)
	}
	if len(only.Except) != 2 || only.Except[0] != "bicycle" || only.Except[1] != "psv" {
		t.Errorf("Except = %v, want [bicycle psv]", only.Except)
	}
	if !only.AppliesTo("car") {
		t.Error("bicycle/psv except must still apply to car")
	}
	if only.AppliesTo("psv") {
		t.Error("psv is listed in except, so the restriction must not apply to psv")
	}

	sum := SummarizeTurnRestrictions(rs)
	if sum.Total != 2 || sum.ByKind["no"] != 1 || sum.ByKind["only"] != 1 {
		t.Errorf("summary = %+v, want 2 total with one no and one only", sum)
	}
	if sum.ViaWay != 0 || sum.WithExcept != 1 {
		t.Errorf("summary via-way/except = %d/%d, want 0/1", sum.ViaWay, sum.WithExcept)
	}
}

// TestParseTurnRestrictionsMalformedRelationSkipped ensures relations that are
// not usable restrictions are ignored instead of producing bogus entries.
func TestParseTurnRestrictionsMalformedRelationSkipped(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<osm version="0.6">
  <relation id="1">
    <member type="way" ref="900" role="from"/>
    <member type="node" ref="101" role="via"/>
    <tag k="type" v="route"/>
  </relation>
  <relation id="2">
    <member type="way" ref="900" role="from"/>
    <member type="node" ref="101" role="via"/>
    <member type="way" ref="901" role="to"/>
    <tag k="type" v="restriction"/>
  </relation>
  <relation id="3">
    <member type="way" ref="900" role="from"/>
    <member type="node" ref="101" role="via"/>
    <tag k="type" v="restriction"/>
    <tag k="restriction" v="no_u_turn"/>
  </relation>
</osm>`
	rs, err := ParseTurnRestrictions(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseTurnRestrictions: %v", err)
	}
	if len(rs) != 0 {
		t.Fatalf("got %d restrictions from malformed input, want 0: %+v", len(rs), rs)
	}
}

// TestParseTurnRestrictionsViaWay covers the longer no-manoeuvre form where via
// is a road way rather than a node.
func TestParseTurnRestrictionsViaWay(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<osm version="0.6">
  <relation id="7">
    <member type="way" ref="900" role="from"/>
    <member type="way" ref="950" role="via"/>
    <member type="way" ref="901" role="to"/>
    <tag k="type" v="restriction"/>
    <tag k="restriction" v="no_u_turn"/>
  </relation>
</osm>`
	rs, err := ParseTurnRestrictions(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseTurnRestrictions: %v", err)
	}
	if len(rs) != 1 {
		t.Fatalf("got %d restrictions, want 1", len(rs))
	}
	if !rs[0].ViaIsWay() || len(rs[0].ViaWays) != 1 || rs[0].ViaWays[0] != 950 {
		t.Fatalf("via ways = %v, want [950]", rs[0].ViaWays)
	}
	if rs[0].ViaNode != 0 {
		t.Errorf("ViaNode = %d, want 0 for a via-way restriction", rs[0].ViaNode)
	}
}

// TestParseTurnRestrictionsRealTokyo validates the parser against restrictions
// exported from the real Kanto extract. It skips when the dump is absent so the
// repository test suite stays self-contained.
func TestParseTurnRestrictionsRealTokyo(t *testing.T) {
	f, err := os.Open("/tmp/turnrestriction/real_restrictions.osm")
	if err != nil {
		t.Skipf("real restriction dump not present: %v", err)
	}
	defer f.Close()
	rs, err := ParseTurnRestrictions(f)
	if err != nil {
		t.Fatal(err)
	}
	sum := SummarizeTurnRestrictions(rs)
	if sum.Total != 5837 {
		t.Fatalf("parsed %d restrictions, want 5837", sum.Total)
	}
	if sum.ByValue["no_right_turn"] != 2207 {
		t.Errorf("no_right_turn = %d, want 2207", sum.ByValue["no_right_turn"])
	}
	if sum.ByValue["no_left_turn"] != 727 {
		t.Errorf("no_left_turn = %d, want 727", sum.ByValue["no_left_turn"])
	}
	if sum.Unknown == 0 {
		t.Error("expected some unmodelled values (charging_only etc.)")
	}
	t.Logf("real Tokyo: %d parsed, unknown=%d viaWay=%d conditional=%d except=%d",
		sum.Total, sum.Unknown, sum.ViaWay, sum.Conditional, sum.WithExcept)
}
