package maren

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// HelmStatusTool executes helm status for a given release.
type HelmStatusTool struct {
	execFn ExecFn
}

// NewHelmStatusTool creates a helm_status tool with the given exec function.
func NewHelmStatusTool(fn ExecFn) *HelmStatusTool {
	return &HelmStatusTool{execFn: fn}
}

func (t *HelmStatusTool) Name() string        { return "helm_status" }
func (t *HelmStatusTool) Description() string { return "Get Helm release status." }

func (t *HelmStatusTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"release": map[string]any{
				"type":        "string",
				"description": "Helm release name",
			},
			"namespace": map[string]any{
				"type":        "string",
				"description": "Namespace of the release (omit for default)",
			},
		},
		Required: []string{"release"},
	}
}

type helmStatusInput struct {
	Release   string `json:"release"`
	Namespace string `json:"namespace"`
}

func (t *HelmStatusTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params helmStatusInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	release := strings.TrimSpace(params.Release)
	if release == "" {
		return "", fmt.Errorf("release is required")
	}
	if strings.HasPrefix(release, "-") {
		return "", fmt.Errorf("invalid release: must not start with '-'")
	}

	namespace := strings.TrimSpace(params.Namespace)
	if namespace != "" && strings.HasPrefix(namespace, "-") {
		return "", fmt.Errorf("invalid namespace: must not start with '-'")
	}

	args := []string{"status", release, "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	out, err := t.execFn(ctx, "helm", args...)
	if err != nil {
		return "", fmt.Errorf("helm: %w", err)
	}

	return sanitiseOutput(string(out)), nil
}
