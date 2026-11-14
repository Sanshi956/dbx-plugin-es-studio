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

// Capabilities returns the matrix for a server version. A zero Version means
// the server could not be identified and is an explicit error, not a guess.
func Capabilities(v Version) (CapabilitySet, error) {
	if v.Major == 0 {
		return CapabilitySet{}, fmt.Errorf("%w: version is empty", ErrUnknownVersion)
	}
	switch {
	case v.Major <= 6:
		return CapabilitySet{TypedAPI: true, Engine: EngineElasticsearch}, nil
	case v.Major == 7:
		return CapabilitySet{
			TypedAPI: true, ILM: true, Engine: EngineElasticsearch,
		}, nil
	default: // 8.x / 9.x and later
		return CapabilitySet{
			TypedAPI: false, DataStreams: true, ComponentTemplates: true,
			PIT: true, ILM: true, ESQL: true, Engine: EngineElasticsearch,
		}, nil
	}
}
