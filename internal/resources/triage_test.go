package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/Azure/aks-mcp/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterTriageResourcesProvidesBoundedJSONContracts(t *testing.T) {
	cfg := config.NewConfig()
	cfg.DefaultAKSResourceID = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/aks-a"
	cfg.AllowNamespaces = "team-a,team-b"
	s := server.NewMCPServer("test", "1")
	RegisterTriageResources(s, cfg)
	resources := s.ListResources()
	if len(resources) != 4 {
		t.Fatalf("resources=%d", len(resources))
	}
	metadata := resources["aks://cluster/metadata"]
	contents, err := metadata.Handler(context.Background(), mcp.ReadResourceRequest{})
	if err != nil || len(contents) != 1 {
		t.Fatalf("metadata read: %v %#v", err, contents)
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok || len(text.Text) > maxResourceBytes || !strings.Contains(text.Text, "aks-a") {
		t.Fatalf("unexpected metadata: %#v", contents[0])
	}
	if _, ok := resources["aks://cluster/policy-posture"]; !ok {
		t.Fatal("policy posture resource missing")
	}
}
