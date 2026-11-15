package es

import (
	"encoding/json"
	"errors"
	"testing"
)

// T4 (RED): version / engine / capability contract, docs/01-PLAN.md.
func TestVersionParse(t *testing.T) {
	cases := []struct {
		raw                  string
		major, minor, patch  int
	}{
		{"8.19.23", 8, 19, 23},
		{"7.17.9", 7, 17, 9},
		{"9.1.0-SNAPSHOT", 9, 1, 0},
		{"8.19", 8, 19, 0},
	}
	for _, c := range cases {
		v, err := ParseVersion(c.raw)
		if err != nil {
			t.Fatalf("ParseVersion(%q) returned error: %v", c.raw, err)
		}
		if v.Major != c.major || v.Minor != c.minor || v.Patch != c.patch {
			t.Fatalf("ParseVersion(%q) = %d.%d.%d, want %d.%d.%d",
				c.raw, v.Major, v.Minor, v.Patch, c.major, c.minor, c.patch)
		}
	}
	if _, err := ParseVersion("not-a-version"); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("invalid version must return ErrUnknownVersion, got %v", err)
	}
}

func TestVersionDetectEngine(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want Engine
	}{
		{"version.distribution", `{"version":{"number":"8.19.23","distribution":"elasticsearch"}}`, EngineElasticsearch},
		{"top-level distribution easysearch", `{"distribution":"easysearch","version":{"number":"1.12.1"}}`, EngineEasysearch},
		{"opensearch", `{"version":{"number":"2.11.0","distribution":"opensearch"}}`, EngineOpenSearch},
		{"default is elasticsearch", `{"version":{"number":"8.19.23"}}`, EngineElasticsearch},
		{"easysearch via build flavor", `{"version":{"number":"1.0.0","build_flavor":"easysearch"}}`, EngineEasysearch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var root map[string]any
			if err := json.Unmarshal([]byte(c.doc), &root); err != nil {
				t.Fatal(err)
			}
			if got := DetectEngine(root); got != c.want {
				t.Fatalf("DetectEngine() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVersionCapabilities(t *testing.T) {
	// Each case is the version that introduced the feature, not a round number:
	// the matrix gates real API calls, so the boundary itself is the assertion.
	cases := []struct {
		raw        string
		typed      bool
		ilm        bool
		component  bool
		dataStream bool
		pit        bool
		esql       bool
	}{
		{"6.8.23", true, true, false, false, false, false},
		{"7.7.0", true, true, false, false, false, false},
		{"7.8.0", true, true, true, false, false, false},
		{"7.9.0", true, true, true, true, false, false},
		{"7.10.0", true, true, true, true, true, false},
		{"7.17.9", true, true, true, true, true, false},
		{"8.0.0", false, true, true, true, true, false},
		{"8.11.0", false, true, true, true, true, true},
		{"8.19.23", false, true, true, true, true, true},
		{"9.0.0", false, true, true, true, true, true},
	}
	for _, tc := range cases {
		v, err := ParseVersion(tc.raw)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.raw, err)
		}
		c, err := Capabilities(v, EngineElasticsearch)
		if err != nil {
			t.Fatalf("capabilities %s: %v", tc.raw, err)
		}
		if c.TypedAPI != tc.typed || c.ILM != tc.ilm || c.ComponentTemplates != tc.component ||
			c.DataStreams != tc.dataStream || c.PIT != tc.pit || c.ESQL != tc.esql {
			t.Fatalf("ES %s: unexpected matrix %+v", tc.raw, c)
		}
		if c.Engine != EngineElasticsearch {
			t.Fatalf("ES %s: engine not carried through: %q", tc.raw, c.Engine)
		}
	}

	if _, err := Capabilities(Version{}, EngineElasticsearch); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("unknown version must return ErrUnknownVersion, got %v", err)
	}
	other, err := Capabilities(Version{Major: 2, Minor: 11}, EngineOpenSearch)
	if err != nil {
		t.Fatal(err)
	}
	if other.Engine != EngineOpenSearch {
		t.Fatalf("engine must travel with the matrix, got %q", other.Engine)
	}
}
