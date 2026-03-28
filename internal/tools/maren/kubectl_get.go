// Package maren provides Maren's Shipwright tools: cluster inspection.
package maren

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// ExecFn executes a command and returns its combined output.
// Production callers pass a function wrapping os/exec.CommandContext;
// tests inject a mock.
type ExecFn func(ctx context.Context, name string, args ...string) ([]byte, error)

// allowedResources is the set of Kubernetes resource types Maren may query.
var allowedResources = map[string]bool{
	"pods":                   true,
	"deployments":            true,
	"services":               true,
	"statefulsets":           true,
	"daemonsets":             true,
	"replicasets":            true,
	"jobs":                   true,
	"cronjobs":               true,
	"ingresses":              true,
	"configmaps":             true,
	"nodes":                  true,
	"namespaces":             true,
	"persistentvolumeclaims": true,
	"events":                 true,
}

// deniedResources is explicitly blocked — defence in depth.
var deniedResources = map[string]bool{
	"secrets":              true,
	"serviceaccounts":      true,
	"roles":                true,
	"rolebindings":         true,
	"clusterroles":         true,
	"clusterrolebindings":  true,
}

// KubectlGetTool executes read-only kubectl get commands.
type KubectlGetTool struct {
	execFn ExecFn
}

// NewKubectlGetTool creates a kubectl_get tool with the given exec function.
func NewKubectlGetTool(fn ExecFn) *KubectlGetTool {
	return &KubectlGetTool{execFn: fn}
}

func (t *KubectlGetTool) Name() string        { return "kubectl_get" }
func (t *KubectlGetTool) Description() string { return "Get Kubernetes resources (read-only)." }

func (t *KubectlGetTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"resource_type": map[string]any{
				"type":        "string",
				"description": "Kubernetes resource type (e.g. pods, deployments, services)",
			},
			"namespace": map[string]any{
				"type":        "string",
				"description": "Namespace to query (omit for cluster-scoped or all namespaces)",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Specific resource name (omit to list all)",
			},
		},
		Required: []string{"resource_type"},
	}
}

type kubectlGetInput struct {
	ResourceType string `json:"resource_type"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
}

func (t *KubectlGetTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params kubectlGetInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	resource := strings.ToLower(strings.TrimSpace(params.ResourceType))
	if resource == "" {
		return "", fmt.Errorf("resource_type is required")
	}

	if deniedResources[resource] {
		return "", fmt.Errorf("resource type %q is not permitted", resource)
	}
	if !allowedResources[resource] {
		return "", fmt.Errorf("resource type %q is not in the allowed list", resource)
	}

	args := []string{"get", resource}
	if ns := strings.TrimSpace(params.Namespace); ns != "" {
		if strings.HasPrefix(ns, "-") {
			return "", fmt.Errorf("invalid namespace: must not start with '-'")
		}
		args = append(args, "-n", ns)
	}
	if name := strings.TrimSpace(params.Name); name != "" {
		if strings.HasPrefix(name, "-") {
			return "", fmt.Errorf("invalid name: must not start with '-'")
		}
		args = append(args, "--", name)
	}

	out, err := t.execFn(ctx, "kubectl", args...)
	if err != nil {
		return "", fmt.Errorf("kubectl: %w", err)
	}

	return sanitiseOutput(string(out)), nil
}

// sensitiveKeys are JSON/YAML key names whose values should be redacted.
var sensitiveKeys = map[string]bool{
	"token":    true,
	"password": true,
	"secret":   true,
	"data":     true,
}

// sensitiveLinePatterns are key names that trigger line removal in plain-text
// output. Matched as whole words to avoid false positives (e.g. "metadata:"
// must not match "data:").
var sensitiveLinePatterns = []string{"token:", "password:", "secret:", "data:"}

// sanitiseOutput redacts sensitive fields from tool output. If the output is
// valid JSON, it performs structured redaction (preserving valid JSON). Otherwise
// it falls back to line-based filtering for plain-text kubectl output.
func sanitiseOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			redactJSON(&parsed)
			out, err := json.Marshal(parsed)
			if err == nil {
				return string(out)
			}
		}
	}

	// Fallback: line-based filtering for plain-text output.
	// Match patterns as whole words to avoid false positives
	// (e.g. "metadata:" must not match the "data:" pattern).
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(line)
		skip := false
		for _, pat := range sensitiveLinePatterns {
			idx := strings.Index(lower, pat)
			if idx == -1 {
				continue
			}
			// Only match if the pattern is at a word boundary: start of
			// line or preceded by whitespace/punctuation (not a letter).
			if idx == 0 || !isWordChar(rune(lower[idx-1])) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

// isWordChar reports whether r is a letter (ASCII).
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// redactJSON recursively walks a parsed JSON value and replaces sensitive
// key values with "[REDACTED]".
func redactJSON(v *any) {
	switch val := (*v).(type) {
	case map[string]any:
		for k, child := range val {
			if sensitiveKeys[strings.ToLower(k)] {
				val[k] = "[REDACTED]"
			} else {
				redactJSON(&child)
				val[k] = child
			}
		}
	case []any:
		for i := range val {
			redactJSON(&val[i])
		}
	}
}
