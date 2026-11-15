package mcp

// Default is the catalogue served by both adapters. It is built once and is
// immutable afterwards; tests assert the size (>= 40), the es_ prefix and that
// every single tool is read-only.
func Default() *Registry {
	tools := make([]Tool, 0, 48)
	tools = append(tools, clusterTools()...)
	tools = append(tools, indexTools()...)
	tools = append(tools, searchTools()...)
	tools = append(tools, opsTools()...)
	return NewRegistry(tools)
}
