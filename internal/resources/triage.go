// Package resources exposes small, bounded MCP resources.  They intentionally
// project server configuration and retained snapshots only; credentials and
// raw kubeconfig data are never resource content.
package resources

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Azure/aks-mcp/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const maxResourceBytes = 16 * 1024

// RegisterTriageResources registers a bounded read-only resource set. Calling
// AddResource is what enables MCP resource capability advertisement; callers
// must not advertise it separately.
func RegisterTriageResources(s *server.MCPServer, cfg *config.ConfigData) {
	registerJSON(s, "aks://cluster/metadata", "AKS cluster metadata", cfg, map[string]any{
		"schema_version": "triage.v1", "resource": "cluster_metadata", "aks_resource_id": cfg.DefaultAKSResourceID,
		"observed_at": time.Now().UTC().Format(time.RFC3339), "freshness": "configuration_at_read",
	})
	registerJSON(s, "aks://cluster/allowed-namespaces", "Allowed Kubernetes namespaces", cfg, map[string]any{
		"schema_version": "triage.v1", "resource": "allowed_namespaces", "namespaces": allowedNamespaces(cfg.AllowNamespaces),
		"observed_at": time.Now().UTC().Format(time.RFC3339), "freshness": "configuration_at_read",
	})
	// These retained-snapshot resources state their freshness explicitly. Live
	// collection remains in the typed tools, avoiding a hidden expensive query
	// merely because a client listed resources.
	registerJSON(s, "aks://cluster/health-snapshot", "Latest retained health snapshot", cfg, map[string]any{
		"schema_version": "triage.v1", "resource": "health_snapshot", "findings": []any{}, "freshness": "not-retained; call aks_cluster_health for a live observation",
	})
	registerJSON(s, "aks://cluster/policy-posture", "Latest retained policy posture", cfg, map[string]any{
		"schema_version": "triage.v1", "resource": "policy_posture", "findings": []any{}, "freshness": "not-retained; call aks_policy_posture for a live observation",
	})
}

func registerJSON(s *server.MCPServer, uri, name string, _ *config.ConfigData, value any) {
	payload, _ := json.Marshal(value)
	if len(payload) > maxResourceBytes {
		payload = []byte(`{"error":"resource exceeds maximum size"}`)
	}
	resource := mcp.NewResource(uri, name, mcp.WithMIMEType("application/json"), mcp.WithResourceDescription("Bounded read-only AKS triage data."))
	s.AddResource(resource, func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return []mcp.ResourceContents{mcp.TextResourceContents{URI: uri, MIMEType: "application/json", Text: string(payload)}}, nil
	})
}

func allowedNamespaces(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	result := []string{}
	for _, namespace := range strings.Split(value, ",") {
		if namespace = strings.TrimSpace(namespace); namespace != "" {
			result = append(result, namespace)
		}
	}
	return result
}
