package es

import "fmt"

// CapabilitySet is the single place where version-dependent API availability
// lives. UI pages and MCP tools read this matrix; no page hardcodes a version.
type CapabilitySet struct {
	TypedAPI           bool   `json:"typedApi"`
	DataStreams        bool   `json:"dataStreams"`
	ComponentTemplates bool   `json:"componentTemplates"`
	PIT                bool   `json:"pit"`
	ILM                bool   `json:"ilm"`
	ESQL               bool   `json:"esql"`
	Engine             Engine `json:"engine"`
}

// Capabilities returns the feature matrix for a server version.
//
// Every gate is the release that actually introduced the API — the matrix is
// what keeps a page from calling something the cluster does not have, so a
// wrong threshold is worse than no matrix at all:
//
//	ILM                      6.6
//	component templates      7.8
//	data streams             7.9
//	point in time (PIT)      7.10
//	ES|QL                    8.11
//
// Typed mappings are the inverse case: present through 7.x, removed in 8.0.
//
// The result is only meaningful for Elasticsearch. Easysearch and OpenSearch
// reuse version numbers with different feature sets, so the engine travels
// with the matrix and the UI must not present a gate as authoritative for them.
func Capabilities(v Version, engine Engine) (CapabilitySet, error) {
	if v.Major == 0 {
		return CapabilitySet{}, fmt.Errorf("%w: version is empty", ErrUnknownVersion)
	}
	if engine == "" {
		engine = EngineUnknown
	}
	atLeast := func(major, minor int) bool {
		if v.Major != major {
			return v.Major > major
		}
		return v.Minor >= minor
	}
	return CapabilitySet{
		Engine:             engine,
		TypedAPI:           v.Major < 8,
		ILM:                atLeast(6, 6),
		ComponentTemplates: atLeast(7, 8),
		DataStreams:        atLeast(7, 9),
		PIT:                atLeast(7, 10),
		ESQL:               atLeast(8, 11),
	}, nil
}
