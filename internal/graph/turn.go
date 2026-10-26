package graph

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// OSM turn restriction support.
//
// A turn restriction is an OSM relation of type=restriction with three kinds of
// members: exactly one "from" way, exactly one "to" way, and one or more "via"
// members (a node for simple restrictions, a way for longer no-manoeuvre
// sequences). The restriction value (no_left_turn, only_straight_on, ...) says
// which transition between the from way and the to way is forbidden or
// mandatory.
//
// The rest of the importer never looks at relations, so today every turn is
// legal and the produced graph is more permissive than the real road network.
// This file only parses and normalises restrictions; it does not change the
// graph and is not wired into ImportOSMXML.

// TurnRestrictionKind distinguishes the two OSM restriction semantics.
type TurnRestrictionKind int

const (
	// NoTurn means the from->to transition is forbidden (no_*_turn, no_entry,
	// no_exit, no_straight_on).
	NoTurn TurnRestrictionKind = iota
	// OnlyTurn means the from->to transition is the only permitted continuation
	// (only_*_turn, only_straight_on).
	OnlyTurn
	// UnknownRestriction is a restriction value this parser does not model.
	UnknownRestriction
)

func (k TurnRestrictionKind) String() string {
	switch k {
	case NoTurn:
		return "no"
	case OnlyTurn:
		return "only"
	default:
		return "unknown"
	}
}

// TurnRestriction is one parsed OSM turn restriction relation.
type TurnRestriction struct {
	RelationID  int64
	Value       string
	Kind        TurnRestrictionKind
	FromWay     int64
	ToWay       int64
	ViaNode     int64
	ViaWays     []int64
	Except      []string
	Conditional string
}

// ViaIsWay reports whether this restriction uses via ways rather than a via node.
func (r TurnRestriction) ViaIsWay() bool { return len(r.ViaWays) > 0 }

// AppliesTo reports whether the restriction applies to the given profile.
func (r TurnRestriction) AppliesTo(profile string) bool {
	if profile == "" {
		profile = "car"
	}
	for _, ex := range r.Except {
		ex = strings.TrimSpace(strings.ToLower(ex))
		switch {
		case ex == profile:
			return false
		case profile == "car" && (ex == "motorcar" || ex == "motor_vehicle" || ex == "vehicle"):
			return false
		case profile == "bike" && (ex == "bicycle" || ex == "vehicle"):
			return false
		case profile == "foot" && (ex == "foot" || ex == "pedestrian"):
			return false
		}
	}
	return true
}

func classifyRestriction(value string) TurnRestrictionKind {
	base := value
	if i := strings.IndexAny(base, " @"); i >= 0 {
		base = base[:i]
	}
	switch {
	case strings.HasPrefix(base, "no_"):
		return NoTurn
	case strings.HasPrefix(base, "only_"):
		return OnlyTurn
	default:
		return UnknownRestriction
	}
}

type osmRelationMember struct {
	Type string `xml:"type,attr"`
	Ref  int64  `xml:"ref,attr"`
	Role string `xml:"role,attr"`
}

type osmRelation struct {
	ID      int64               `xml:"id,attr"`
	Members []osmRelationMember `xml:"member"`
	Tags    []osmTag            `xml:"tag"`
}

// ParseTurnRestrictions reads OSM XML and returns every parseable turn
// restriction. It ignores nodes and ways, so it is cheap to run as a focused
// pass over an extract.
func ParseTurnRestrictions(r io.Reader) ([]TurnRestriction, error) {
	dec := xml.NewDecoder(r)
	var out []TurnRestriction
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse OSM relations: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "relation" {
			continue
		}
		var rel osmRelation
		if err := dec.DecodeElement(&rel, &se); err != nil {
			return nil, err
		}
		restriction, ok, err := restrictionFromRelation(rel)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, restriction)
		}
	}
	return out, nil
}

// restrictionFromRelation converts one relation. ok is false when the relation
// is not a usable turn restriction.
func restrictionFromRelation(rel osmRelation) (TurnRestriction, bool, error) {
	tags := tagMap(rel.Tags)
	if tags["type"] != "restriction" {
		return TurnRestriction{}, false, nil
	}
	value := tags["restriction"]
	if value == "" {
		for _, k := range []string{"restriction:motorcar", "restriction:motor_vehicle"} {
			if v := tags[k]; v != "" {
				value = v
				break
			}
		}
	}
	if value == "" {
		value = tags["restriction:conditional"]
		if value == "" {
			return TurnRestriction{}, false, nil
		}
	}

	var fromIDs, toIDs []int64
	var viaNodes, viaWays []int64
	for _, m := range rel.Members {
		switch m.Role {
		case "from":
			if m.Type == "way" {
				fromIDs = append(fromIDs, m.Ref)
			}
		case "to":
			if m.Type == "way" {
				toIDs = append(toIDs, m.Ref)
			}
		case "via":
			switch m.Type {
			case "node":
				viaNodes = append(viaNodes, m.Ref)
			case "way":
				viaWays = append(viaWays, m.Ref)
			}
		}
	}
	if len(fromIDs) != 1 || len(toIDs) != 1 {
		return TurnRestriction{}, false, nil
	}
	if len(viaNodes) == 0 && len(viaWays) == 0 {
		return TurnRestriction{}, false, nil
	}

	res := TurnRestriction{
		RelationID:  rel.ID,
		Value:       value,
		Kind:        classifyRestriction(value),
		FromWay:     fromIDs[0],
		ToWay:       toIDs[0],
		Conditional: tags["restriction:conditional"],
	}
	if len(viaNodes) > 0 {
		res.ViaNode = viaNodes[0]
	}
	if len(viaWays) > 0 {
		res.ViaWays = viaWays
	}
	if ex := tags["except"]; ex != "" {
		for _, part := range strings.Split(ex, ";") {
			if part = strings.TrimSpace(part); part != "" {
				res.Except = append(res.Except, part)
			}
		}
	}
	return res, true, nil
}

// RestrictionSummary aggregates restriction counts for reporting.
type RestrictionSummary struct {
	Total       int
	Unknown     int
	ViaWay      int
	Conditional int
	WithExcept  int
	ByValue     map[string]int
	ByKind      map[string]int
}

// SummarizeTurnRestrictions counts restrictions by value and by kind.
func SummarizeTurnRestrictions(rs []TurnRestriction) RestrictionSummary {
	s := RestrictionSummary{
		Total:   len(rs),
		ByValue: make(map[string]int),
		ByKind:  make(map[string]int),
	}
	for _, r := range rs {
		s.ByValue[r.Value]++
		s.ByKind[r.Kind.String()]++
		if r.Kind == UnknownRestriction {
			s.Unknown++
		}
		if r.ViaIsWay() {
			s.ViaWay++
		}
		if r.Conditional != "" {
			s.Conditional++
		}
		if len(r.Except) > 0 {
			s.WithExcept++
		}
	}
	return s
}
