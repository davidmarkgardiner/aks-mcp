package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Azure/aks-mcp/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	clusterHealthSchemaVersion = "triage.v1"
	maxClusterHealthEvents     = 50
)

var safeScopeIdentifier = regexp.MustCompile(`^[A-Za-z0-9._()/:-]+$`)

// ClusterHealthRequest scopes a read-only Resource Health snapshot. It does
// not accept credentials, kubeconfig content, a command, or a mutation mode.
type ClusterHealthRequest struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"Azure subscription ID"`
	ResourceGroup  string `json:"resource_group" jsonschema:"AKS resource group"`
	ClusterName    string `json:"cluster_name" jsonschema:"AKS cluster name"`
	StartTime      string `json:"start_time" jsonschema:"RFC3339 start time"`
	EndTime        string `json:"end_time,omitempty" jsonschema:"Optional RFC3339 end time"`
}

// ClusterHealthScope identifies the observed AKS cluster without exposing
// credentials or kubeconfig material.
type ClusterHealthScope struct {
	SubscriptionID string `json:"subscription_id"`
	ResourceGroup  string `json:"resource_group"`
	ClusterName    string `json:"cluster_name"`
}

// ClusterHealthEvent is a bounded, safe projection of an Azure Resource
// Health activity-log event.
type ClusterHealthEvent struct {
	ObservedAt string `json:"observed_at,omitempty"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

// ClusterHealthResult is the versioned structured result returned to agents.
// It describes only the Resource Health signal, not a blanket claim that every
// Kubernetes workload in the cluster is healthy.
type ClusterHealthResult struct {
	SchemaVersion string               `json:"schema_version"`
	Signal        string               `json:"signal"`
	Scope         ClusterHealthScope   `json:"scope"`
	ObservedAt    string               `json:"observed_at"`
	Events        []ClusterHealthEvent `json:"events"`
	Truncated     bool                 `json:"truncated"`
}

// RegisterClusterHealth registers the new typed, read-only triage tool. The
// legacy aks_monitoring tool remains unchanged for backwards compatibility.
func RegisterClusterHealth() mcp.Tool {
	return mcp.NewTool("aks_cluster_health",
		mcp.WithDescription("Return a bounded, structured, read-only Azure Resource Health snapshot for one AKS cluster."),
		mcp.WithReadOnlyHintAnnotation(true),
		// The same read-only handler may be executed as an MCP Task for slow
		// Resource Health queries. mcp-go supplies the task handle, bounded
		// progress state, polling endpoint, cancellation and TTL handling.
		mcp.WithTaskSupport(mcp.TaskSupportOptional),
		mcp.WithInputSchema[ClusterHealthRequest](),
		mcp.WithOutputSchema[ClusterHealthResult](),
	)
}

// GetClusterHealthHandler adapts the existing read-only Resource Health query
// into the stable triage.v1 result contract.
func GetClusterHealthHandler(cfg *config.ConfigData) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, request ClusterHealthRequest) (ClusterHealthResult, error) {
		if err := validateClusterHealthRequest(request); err != nil {
			return ClusterHealthResult{}, err
		}

		raw, err := HandleResourceHealthQuery(ctx, map[string]interface{}{
			"subscription_id": request.SubscriptionID,
			"resource_group":  request.ResourceGroup,
			"cluster_name":    request.ClusterName,
			"start_time":      request.StartTime,
			"end_time":        request.EndTime,
		}, cfg)
		if err != nil {
			return ClusterHealthResult{}, err
		}
		return newClusterHealthResult(request, raw, time.Now().UTC())
	})
}

func validateClusterHealthRequest(request ClusterHealthRequest) error {
	for name, value := range map[string]string{
		"subscription_id": request.SubscriptionID,
		"resource_group":  request.ResourceGroup,
		"cluster_name":    request.ClusterName,
	} {
		if value == "" || !safeScopeIdentifier.MatchString(value) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if _, err := time.Parse(time.RFC3339, request.StartTime); err != nil {
		return fmt.Errorf("invalid start_time: expected RFC3339")
	}
	if request.EndTime != "" {
		if _, err := time.Parse(time.RFC3339, request.EndTime); err != nil {
			return fmt.Errorf("invalid end_time: expected RFC3339")
		}
	}
	return nil
}

type resourceHealthEvent struct {
	EventTimestamp string `json:"eventTimestamp"`
	Properties     struct {
		CurrentHealthStatus string `json:"currentHealthStatus"`
		Title               string `json:"title"`
		Cause               string `json:"cause"`
	} `json:"properties"`
}

func newClusterHealthResult(request ClusterHealthRequest, raw string, observedAt time.Time) (ClusterHealthResult, error) {
	var source []resourceHealthEvent
	if err := json.Unmarshal([]byte(raw), &source); err != nil {
		return ClusterHealthResult{}, fmt.Errorf("invalid Resource Health response")
	}
	sort.SliceStable(source, func(i, j int) bool { return source[i].EventTimestamp > source[j].EventTimestamp })
	truncated := len(source) > maxClusterHealthEvents
	if truncated {
		source = source[:maxClusterHealthEvents]
	}
	events := make([]ClusterHealthEvent, 0, len(source))
	for _, event := range source {
		summary := strings.TrimSpace(strings.Join([]string{event.Properties.Title, event.Properties.Cause}, ": "))
		summary = strings.Trim(summary, ": ")
		events = append(events, ClusterHealthEvent{ObservedAt: event.EventTimestamp, Status: event.Properties.CurrentHealthStatus, Summary: summary})
	}
	return ClusterHealthResult{
		SchemaVersion: clusterHealthSchemaVersion,
		Signal:        "azure_resource_health_activity_log",
		Scope:         ClusterHealthScope{SubscriptionID: request.SubscriptionID, ResourceGroup: request.ResourceGroup, ClusterName: request.ClusterName},
		ObservedAt:    observedAt.Format(time.RFC3339),
		Events:        events,
		Truncated:     truncated,
	}, nil
}
