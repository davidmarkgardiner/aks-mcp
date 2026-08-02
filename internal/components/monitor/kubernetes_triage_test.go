package monitor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Azure/aks-mcp/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
)

func testTriageConfig() *config.ConfigData {
	cfg := config.NewConfig()
	cfg.DefaultAKSResourceID = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/test"
	return cfg
}

func TestTriageProjectionContracts(t *testing.T) {
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	cfg := testTriageConfig()
	request := KubernetesTriageRequest{Namespace: "team-a", Limit: 1}

	nodes, err := newNodePressureResult(request, cfg, []byte(`{"items":[{"metadata":{"name":"node-b"},"status":{"conditions":[{"type":"MemoryPressure","status":"True"}]}},{"metadata":{"name":"node-a"},"status":{"conditions":[{"type":"DiskPressure","status":"True"}]}}]}`), now)
	if err != nil || nodes.SchemaVersion != kubernetesTriageSchema || nodes.Signal != "kubernetes_node_pressure" || len(nodes.Findings) != 1 || !nodes.Truncated {
		t.Fatalf("unexpected node contract: result=%#v err=%v", nodes, err)
	}

	workloads, err := newWorkloadFailureResult(request, cfg, []byte(`{"items":[{"metadata":{"name":"api","namespace":"team-a"},"status":{"phase":"Running","containerStatuses":[{"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}]}`), now)
	if err != nil || workloads.Findings[0].Reason != "CrashLoopBackOff" || workloads.Scope.Namespace != "team-a" {
		t.Fatalf("unexpected workload contract: result=%#v err=%v", workloads, err)
	}

	policy, err := newPolicyPostureResult(request, cfg, []byte(`{"items":[{"metadata":{"name":"report","namespace":"team-a"},"summary":{"fail":2,"warn":1}}]}`), now)
	if err != nil || policy.Findings[0].Severity != "error" || !strings.Contains(policy.Findings[0].Reason, "2 policy failures") {
		t.Fatalf("unexpected policy contract: result=%#v err=%v", policy, err)
	}

	deployments, err := newDeploymentHistoryResult(request, cfg, []byte(`{"items":[{"metadata":{"name":"api","namespace":"team-a","annotations":{"deployment.kubernetes.io/revision":"7"}},"status":{"unavailableReplicas":2}}]}`), now)
	if err != nil || !strings.Contains(deployments.Findings[0].Reason, "revision 7") || deployments.ObservedAt != "2026-08-02T10:00:00Z" {
		t.Fatalf("unexpected deployment contract: result=%#v err=%v", deployments, err)
	}
}

func TestTriageProjectionEmptyResultsAreStructuredAndSafe(t *testing.T) {
	cfg := testTriageConfig()
	result, err := newWorkloadFailureResult(KubernetesTriageRequest{}, cfg, []byte(`{"items":[]}`), time.Now().UTC())
	if err != nil || result.Findings == nil || len(result.Findings) != 0 || result.Truncated {
		t.Fatalf("empty result must be a usable structured success: result=%#v err=%v", result, err)
	}
}

func TestTriageHandlersUseFixedReadOnlyQueriesAndSafeErrors(t *testing.T) {
	cfg := testTriageConfig()
	var got []string
	reader := func(_ context.Context, args ...string) ([]byte, error) {
		got = args
		return []byte(`{"items":[]}`), nil
	}
	request := mcp.CallToolRequest{}
	request.Params.Name = "aks_workload_failures"
	request.Params.Arguments = map[string]any{"namespace": "team-a", "limit": 2}
	result, err := GetWorkloadFailuresHandlerWithReader(cfg, reader)(context.Background(), request)
	if err != nil || result.IsError || strings.Join(got, " ") != "get pods --namespace team-a -o json" {
		t.Fatalf("unexpected fixed workload query: result=%#v args=%v err=%v", result, got, err)
	}

	denied := func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New("Forbidden: secrets are forbidden")
	}
	result, err = GetWorkloadFailuresHandlerWithReader(cfg, denied)(context.Background(), request)
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "permission denied") || strings.Contains(result.Content[0].(mcp.TextContent).Text, "secrets") {
		t.Fatalf("permission failure leaked or was not structured: %#v", result)
	}
}

func TestTriageNamespaceAllowListAndToolSchemas(t *testing.T) {
	cfg := testTriageConfig()
	cfg.AllowNamespaces = "team-a,team-b"
	if err := validateKubernetesTriageRequest(KubernetesTriageRequest{}, cfg, true); err == nil {
		t.Fatal("all namespaces must be rejected when server has an allow-list")
	}
	if err := validateKubernetesTriageRequest(KubernetesTriageRequest{Namespace: "other"}, cfg, true); err == nil {
		t.Fatal("unapproved namespace was accepted")
	}
	for _, tool := range []mcp.Tool{RegisterNodePressure(), RegisterWorkloadFailures(), RegisterPolicyPosture(), RegisterDeploymentHistory()} {
		if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint || tool.OutputSchema.Type == "" || len(tool.OutputSchema.Properties) == 0 {
			t.Fatalf("tool lacks typed read-only schema: %#v", tool)
		}
	}
}
