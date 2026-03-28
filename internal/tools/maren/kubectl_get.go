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
	if params.Namespace != "" {
		args = append(args, "-n", params.Namespace)
	}
	if params.Name != "" {
		args = append(args, params.Name)
	}

	out, err := t.execFn(ctx, "kubectl", args...)
	if err != nil {
		return "", fmt.Errorf("kubectl: %w", err)
	}

	return sanitiseOutput(string(out)), nil
}

// sensitivePatterns are substrings that trigger line removal in tool output.
var sensitivePatterns = []string{"token:", "password:", "secret:"}

// sanitiseOutput removes lines containing sensitive key patterns.
func sanitiseOutput(output string) string {
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(line)
		skip := false
		for _, pat := range sensitivePatterns {
			if strings.Contains(lower, pat) {
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
