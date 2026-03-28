// Package maren provides Maren's Shipwright tools: cluster inspection.
package maren

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// ExecFn executes a command and returns its standard output (stdout).
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
				"description": "Namespace to query (omit to use the current-context namespace; cluster-scoped resources ignore this)",
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
// Includes manifest/notes because helm status embeds full YAML manifests
// as string values that may contain secrets.
var sensitiveKeys = map[string]bool{
	"token":    true,
	"password": true,
	"secret":   true,
	"manifest": true,
	"notes":    true,
}

// sensitiveLinePatterns are key names that trigger line removal in plain-text
// output. Matched as whole words to avoid false positives where a pattern
// appears as part of a longer word.
var sensitiveLinePatterns = []string{"token:", "password:", "secret:"}

// sanitiseOutput redacts sensitive fields from tool output. If the output
// contains a JSON object or array, it performs structured redaction (preserving
// valid JSON). Otherwise it falls back to line-based filtering for plain-text
// kubectl output.
func sanitiseOutput(output string) string {
	// Try to locate and parse JSON within the output. stderr warnings may
	// precede the JSON body, so search for the first '{' or '['.
	if jsonStart := findJSONStart(output); jsonStart >= 0 {
		prefix := output[:jsonStart]
		jsonPart := output[jsonStart:]
		var parsed any
		if err := json.Unmarshal([]byte(jsonPart), &parsed); err == nil {
			redactJSON(&parsed)
			out, err := json.Marshal(parsed)
			if err == nil {
				// Preserve any non-JSON prefix (stderr warnings) with
				// line-based sanitisation.
				if prefix != "" {
					return sanitiseLines(prefix) + string(out)
				}
				return string(out)
			}
		}
	}

	// Fallback: line-based filtering for plain-text output.
	return sanitiseLines(output)
}

// findJSONStart returns the index of the first '{' or '[' in s, or -1.
func findJSONStart(s string) int {
	for i, r := range s {
		if r == '{' || r == '[' {
			return i
		}
	}
	return -1
}

// sanitiseLines removes lines containing sensitive key patterns, using
// whole-word matching to avoid false positives where a pattern appears as
// part of a longer word.
func sanitiseLines(output string) string {
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
