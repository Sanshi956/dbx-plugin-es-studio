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
	v7, err := ParseVersion("7.17.9")
	if err != nil {
		t.Fatal(err)
	}
	c7, err := Capabilities(v7)
	if err != nil {
		t.Fatal(err)
	}
	if !c7.TypedAPI || c7.DataStreams {
		t.Fatalf("ES 7.x: typedAPI must be true and dataStreams false, got %+v", c7)
	}

	for _, raw := range []string{"8.19.23", "9.0.0"} {
		v, err := ParseVersion(raw)
		if err != nil {
			t.Fatal(err)
		}
		c, err := Capabilities(v)
		if err != nil {
			t.Fatal(err)
		}
		if c.TypedAPI || !c.DataStreams || !c.ComponentTemplates || !c.PIT {
			t.Fatalf("ES %s: unexpected capability matrix %+v", raw, c)
		}
	}

	if _, err := Capabilities(Version{}); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("unknown version must return ErrUnknownVersion, got %v", err)
	}
}
