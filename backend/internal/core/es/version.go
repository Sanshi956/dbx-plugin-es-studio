package es

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrUnknownVersion is returned when a version string cannot be interpreted.
var ErrUnknownVersion = errors.New("unknown elasticsearch version")

// Engine identifies the server distribution. Easysearch / OpenSearch share most
// REST shapes but must never be declared compatible without real API tests.
type Engine string

const (
	EngineElasticsearch Engine = "elasticsearch"
	EngineEasysearch    Engine = "easysearch"
	EngineOpenSearch    Engine = "opensearch"
	EngineUnknown       Engine = "unknown"
)

// Version is the parsed server version, kept separate from raw vendor strings.
type Version struct {
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Patch int    `json:"patch"`
	Raw   string `json:"raw"`
}

// ParseVersion accepts "8.19.23", "8.19", and pre-release suffixes such as
// "9.1.0-SNAPSHOT". Anything else is ErrUnknownVersion.
func ParseVersion(raw string) (Version, error) {
	core := raw
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, fmt.Errorf("%w: %q", ErrUnknownVersion, raw)
	}
	nums := make([]int, 3)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("%w: %q", ErrUnknownVersion, raw)
		}
		nums[i] = n
	}
	if nums[0] == 0 {
		return Version{}, fmt.Errorf("%w: %q", ErrUnknownVersion, raw)
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Raw: raw}, nil
}

// DetectEngine reads distribution hints from the GET / response. Unknown
// distributions default to elasticsearch, which is the conservative behaviour
// for the adapter (capability detection still gates every API call).
func DetectEngine(root map[string]any) Engine {
	candidates := make([]string, 0, 4)
	if version, ok := root["version"].(map[string]any); ok {
		for _, key := range []string{"distribution", "build_flavor", "build_type"} {
			if s, ok := version[key].(string); ok {
				candidates = append(candidates, s)
			}
		}
	}
	for _, key := range []string{"distribution", "engine", "flavor"} {
		if s, ok := root[key].(string); ok {
			candidates = append(candidates, s)
		}
	}
	for _, candidate := range candidates {
		value := strings.ToLower(candidate)
		switch {
		case strings.Contains(value, "opensearch"):
			return EngineOpenSearch
		case strings.Contains(value, "easysearch"):
			return EngineEasysearch
		case strings.Contains(value, "elasticsearch"):
			return EngineElasticsearch
		}
	}
	return EngineElasticsearch
}
