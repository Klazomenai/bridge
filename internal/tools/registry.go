// Package tools provides the tool registry and executor interface for crew
// member tool use. Tools are registered at startup and looked up per crew
// member based on the tools declared in crew.yaml.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// ToolDefinition is the interface that all crew tools implement.
type ToolDefinition interface {
	// Name returns the unique tool identifier (matches crew.yaml tools entries).
	Name() string
	// Description returns a human-readable description for Claude's system prompt.
	Description() string
	// InputSchema returns the JSON Schema describing the tool's input parameters.
	InputSchema() anthropic.ToolInputSchemaParam
	// Execute runs the tool with the given JSON input and returns the result text.
	// Errors are converted to isError tool results by the caller.
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// MutationAware is an optional interface a tool may implement to declare
// whether its invocation mutates external state. The audit log emits a
// `mutation` field for every invocation; tools that do NOT implement this
// interface are conservatively treated as read-only (mutation=false).
//
// Write tools (e.g. a future gh_issue_close) should implement Mutation()
// returning true so the audit log surfaces an irreversible action.
type MutationAware interface {
	Mutation() bool
}

// IsMutation reports whether tool t represents a state-mutating operation.
// Returns false when t does not implement MutationAware.
func IsMutation(t ToolDefinition) bool {
	if m, ok := t.(MutationAware); ok {
		return m.Mutation()
	}
	return false
}

// Registry holds all registered tools and provides lookup by name and crew.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ToolDefinition
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]ToolDefinition),
	}
}

// Register adds a tool to the registry. Panics if a tool with the same name
// is already registered (programming error, not runtime).
func (r *Registry) Register(tool ToolDefinition) {
	if tool == nil {
		panic("tools: cannot register nil tool")
	}
	name := tool.Name()
	if name == "" {
		panic("tools: cannot register tool with empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		panic(fmt.Sprintf("tools: duplicate registration for %q", name))
	}
	r.tools[name] = tool
}

// Get returns the tool by name, or nil if not found.
func (r *Registry) Get(name string) ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// Has reports whether a tool with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// Execute looks up a tool by name and runs it. Returns an error if the tool
// is not registered.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	tool := r.Get(name)
	if tool == nil {
		return "", fmt.Errorf("unknown tool: %q", name)
	}
	return tool.Execute(ctx, input)
}

// ForCrew returns the Anthropic ToolUnionParam slice for a crew member's
// declared tools. Unknown tool names are silently skipped (validation should
// catch these at startup via crew.Registry.ValidateTools).
func (r *Registry) ForCrew(toolNames []string) []anthropic.ToolUnionParam {
	// Copy tool references under lock, then build params without holding it.
	r.mu.RLock()
	matched := make([]ToolDefinition, 0, len(toolNames))
	for _, name := range toolNames {
		if tool, ok := r.tools[name]; ok {
			matched = append(matched, tool)
		}
	}
	r.mu.RUnlock()

	params := make([]anthropic.ToolUnionParam, 0, len(matched))
	for _, tool := range matched {
		params = append(params, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        tool.Name(),
				Description: anthropic.String(tool.Description()),
				InputSchema: tool.InputSchema(),
			},
		})
	}
	return params
}

// Names returns all registered tool names in sorted order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
