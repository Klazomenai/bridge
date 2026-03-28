package maren_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/maren"
)

// mockExec returns a mock ExecFn that records calls and returns canned output.
func mockExec(output string, err error) (maren.ExecFn, *[]string) {
	var calls []string
	fn := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte(output), err
	}
	return fn, &calls
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// --- KubectlGetTool tests ---

func TestKubectlGetAllowedResource(t *testing.T) {
	fn, calls := mockExec("NAME   READY\nnginx  1/1\n", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "pods", "namespace": "default"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "nginx") {
		t.Errorf("expected output to contain nginx, got %q", out)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*calls))
	}
	if (*calls)[0] != "kubectl get pods -n default" {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestKubectlGetWithName(t *testing.T) {
	fn, calls := mockExec("NAME   READY\nnginx  1/1\n", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "pods", "namespace": "kube-system", "name": "coredns"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if (*calls)[0] != "kubectl get pods -n kube-system -- coredns" {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestKubectlGetNoNamespace(t *testing.T) {
	fn, calls := mockExec("node1\n", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "nodes"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if (*calls)[0] != "kubectl get nodes" {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestKubectlGetDeniedResource(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewKubectlGetTool(fn)

	for _, resource := range []string{"secrets", "serviceaccounts", "roles", "rolebindings", "clusterroles", "clusterrolebindings"} {
		t.Run(resource, func(t *testing.T) {
			input := mustJSON(t, map[string]string{"resource_type": resource})
			_, err := tool.Execute(t.Context(), input)
			if err == nil {
				t.Fatal("expected error for denied resource")
			}
			if !strings.Contains(err.Error(), "not permitted") {
				t.Errorf("expected 'not permitted' error, got: %v", err)
			}
		})
	}
}

func TestKubectlGetUnknownResource(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "customresources"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for unknown resource")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Errorf("expected 'not in the allowed list' error, got: %v", err)
	}
}

func TestKubectlGetEmptyResourceType(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": ""})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for empty resource_type")
	}
}

func TestKubectlGetExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("connection refused"))
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "pods"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected exec error propagated, got: %v", err)
	}
}

func TestKubectlGetSanitisesOutput(t *testing.T) {
	raw := "NAME   DATA\napp    config\ndb     token: abc123\nsvc    password: secret\n"
	fn, _ := mockExec(raw, nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "configmaps"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "token:") {
		t.Error("output should not contain 'token:' lines")
	}
	if strings.Contains(out, "password:") {
		t.Error("output should not contain 'password:' lines")
	}
	if !strings.Contains(out, "app") {
		t.Error("output should still contain safe lines")
	}
}

func TestKubectlGetNamespaceFlagInjection(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "pods", "namespace": "-A"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for namespace starting with '-'")
	}
	if !strings.Contains(err.Error(), "must not start with '-'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestKubectlGetNameFlagInjection(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "pods", "name": "-o yaml"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for name starting with '-'")
	}
	if !strings.Contains(err.Error(), "must not start with '-'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestKubectlGetSanitisesJSONOutput(t *testing.T) {
	raw := `{"items":[{"metadata":{"name":"app"},"data":{"key":"val"},"spec":{"token":"abc123","password":"hunter2","safe":"keep"}}]}`
	fn, _ := mockExec(raw, nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "configmaps"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "abc123") {
		t.Error("output should not contain token value")
	}
	if strings.Contains(out, "hunter2") {
		t.Error("output should not contain password value")
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Error("expected [REDACTED] in output")
	}
	if !strings.Contains(out, "keep") {
		t.Error("safe values should be preserved")
	}
}

func TestKubectlGetDoesNotOverSanitiseMetadata(t *testing.T) {
	raw := "NAME   DATA\napp    config\nmetadata: {}\ndb     token: abc123\n"
	fn, _ := mockExec(raw, nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "configmaps"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "token:") {
		t.Error("output should not contain 'token:' lines")
	}
	if !strings.Contains(out, "metadata:") {
		t.Error("output should still contain 'metadata:' lines")
	}
}

func TestKubectlGetCaseInsensitiveResource(t *testing.T) {
	fn, calls := mockExec("ok\n", nil)
	tool := maren.NewKubectlGetTool(fn)

	input := mustJSON(t, map[string]string{"resource_type": "Pods"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if (*calls)[0] != "kubectl get pods" {
		t.Errorf("expected lowercased resource, got: %q", (*calls)[0])
	}
}

func TestKubectlGetInterface(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewKubectlGetTool(fn)
	if tool.Name() != "kubectl_get" {
		t.Errorf("Name() = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	schema := tool.InputSchema()
	if schema.Properties == nil {
		t.Error("InputSchema().Properties should not be nil")
	}
}

// --- HelmStatusTool tests ---

func TestHelmStatusBasic(t *testing.T) {
	fn, calls := mockExec(`{"name":"argocd","namespace":"argocd","info":{"status":"deployed"}}`, nil)
	tool := maren.NewHelmStatusTool(fn)

	input := mustJSON(t, map[string]string{"release": "argocd", "namespace": "argocd"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "deployed") {
		t.Errorf("expected 'deployed' in output, got %q", out)
	}
	if (*calls)[0] != "helm status argocd -o json -n argocd" {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestHelmStatusNoNamespace(t *testing.T) {
	fn, calls := mockExec(`{"name":"app"}`, nil)
	tool := maren.NewHelmStatusTool(fn)

	input := mustJSON(t, map[string]string{"release": "app"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if (*calls)[0] != "helm status app -o json" {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestHelmStatusReleaseFlagInjection(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewHelmStatusTool(fn)

	input := mustJSON(t, map[string]string{"release": "--kubeconfig=/etc/shadow"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for release starting with '-'")
	}
	if !strings.Contains(err.Error(), "must not start with '-'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHelmStatusEmptyRelease(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewHelmStatusTool(fn)

	input := mustJSON(t, map[string]string{"release": ""})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for empty release")
	}
}

func TestHelmStatusExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("release not found"))
	tool := maren.NewHelmStatusTool(fn)

	input := mustJSON(t, map[string]string{"release": "missing"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
}

func TestHelmStatusSanitisesJSONOutput(t *testing.T) {
	raw := `{"name":"app","config":{"password":"hunter2","token":"abc","dbHost":"postgres:5432"}}`
	fn, _ := mockExec(raw, nil)
	tool := maren.NewHelmStatusTool(fn)

	input := mustJSON(t, map[string]string{"release": "app"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, "hunter2") {
		t.Error("output should not contain password value")
	}
	if strings.Contains(out, "abc") {
		t.Error("output should not contain token value")
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Error("expected [REDACTED] in output")
	}
	if !strings.Contains(out, "postgres:5432") {
		t.Error("safe values should be preserved")
	}
}

func TestHelmStatusInterface(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := maren.NewHelmStatusTool(fn)
	if tool.Name() != "helm_status" {
		t.Errorf("Name() = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	schema := tool.InputSchema()
	if schema.Properties == nil {
		t.Error("InputSchema().Properties should not be nil")
	}
}
