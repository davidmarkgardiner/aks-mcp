package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestNewClusterHealthResult_ProjectsAndBoundsEvents(t *testing.T) {
	request := ClusterHealthRequest{SubscriptionID: "sub-1", ResourceGroup: "rg_1", ClusterName: "cluster-1", StartTime: "2026-07-29T00:00:00Z"}
	raw := `[
  {"eventTimestamp":"2026-07-29T02:00:00Z","properties":{"currentHealthStatus":"Available","title":"Recovered","cause":"Platform event resolved"}},
  {"eventTimestamp":"2026-07-29T01:00:00Z","properties":{"currentHealthStatus":"Degraded","title":"Degraded","cause":"Platform event"}}
]`
	result, err := newClusterHealthResult(request, raw, time.Date(2026, 7, 29, 3, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("newClusterHealthResult() error = %v", err)
	}
	if result.SchemaVersion != clusterHealthSchemaVersion || result.Signal != "azure_resource_health_activity_log" {
		t.Fatalf("unexpected contract identity: %#v", result)
	}
	if result.Scope.ClusterName != "cluster-1" || result.ObservedAt != "2026-07-29T03:00:00Z" {
		t.Fatalf("unexpected scope or observation time: %#v", result)
	}
	if len(result.Events) != 2 || result.Events[0].Status != "Available" || result.Events[0].Summary != "Recovered: Platform event resolved" {
		t.Fatalf("unexpected event projection: %#v", result.Events)
	}
	if result.Truncated {
		t.Fatal("small result must not be truncated")
	}
}

func TestNewClusterHealthResult_RejectsInvalidAzureResponse(t *testing.T) {
	_, err := newClusterHealthResult(ClusterHealthRequest{}, `{not-json`, time.Now())
	if err == nil || strings.Contains(err.Error(), "{not-json") {
		t.Fatalf("expected a safe invalid-response error, got %v", err)
	}
}

func TestValidateClusterHealthRequest(t *testing.T) {
	valid := ClusterHealthRequest{SubscriptionID: "00000000-0000-0000-0000-000000000000", ResourceGroup: "rg_safe", ClusterName: "cluster.safe", StartTime: "2026-07-29T00:00:00Z"}
	if err := validateClusterHealthRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	unsafe := valid
	unsafe.ClusterName = "cluster; az account show"
	if err := validateClusterHealthRequest(unsafe); err == nil {
		t.Fatal("unsafe scope identifier was accepted")
	}
	invalidTime := valid
	invalidTime.StartTime = "yesterday"
	if err := validateClusterHealthRequest(invalidTime); err == nil {
		t.Fatal("invalid timestamp was accepted")
	}
}

func TestRegisterClusterHealth_TypedReadOnlyContract(t *testing.T) {
	tool := RegisterClusterHealth(true)
	if tool.Name != "aks_cluster_health" || tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("tool is not declared as typed read-only triage: %#v", tool)
	}
	if tool.OutputSchema.Type == "" || len(tool.OutputSchema.Properties) == 0 {
		t.Fatalf("tool has no generated structured output schema: %#v", tool.OutputSchema)
	}
	if tool.Execution == nil || tool.Execution.TaskSupport != mcp.TaskSupportOptional {
		t.Fatalf("cluster-health must expose optional MCP task support: %#v", tool.Execution)
	}
}

func TestRegisterClusterHealth_DoesNotAdvertiseVolatileTasks(t *testing.T) {
	tool := RegisterClusterHealth(false)
	if tool.Execution != nil {
		t.Fatalf("task support must be absent without durable storage: %#v", tool.Execution)
	}
}
