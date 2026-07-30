package k8s

import (
	"encoding/json"
	"testing"
)

func TestCreateCallKubectlTool_ResourceIDRequired_WhenNoDefault(t *testing.T) {
	tool := createCallKubectlTool("readonly", "", nil, "")

	schemaBytes, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("failed to marshal input schema: %v", err)
	}

	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}

	found := false
	for _, r := range schema.Required {
		if r == "aks_resource_id" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected aks_resource_id to be required when no default provided, required fields: %v", schema.Required)
	}
}

func TestCreateCallKubectlTool_ResourceIDOptional_WhenDefaultSet(t *testing.T) {
	tool := createCallKubectlTool("readonly", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/cluster", nil, "")

	schemaBytes, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("failed to marshal input schema: %v", err)
	}

	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}

	for _, r := range schema.Required {
		if r == "aks_resource_id" {
			t.Errorf("aks_resource_id should be optional when default is configured, but found in required: %v", schema.Required)
		}
	}
}

func TestCreateCallKubectlTool_CommandAlwaysRequired(t *testing.T) {
	tests := []struct {
		name              string
		defaultResourceID string
	}{
		{"no default", ""},
		{"with default", "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/cluster"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := createCallKubectlTool("readonly", tt.defaultResourceID, nil, "")

			schemaBytes, _ := json.Marshal(tool.InputSchema)
			var schema struct {
				Required []string `json:"required"`
			}
			_ = json.Unmarshal(schemaBytes, &schema)

			found := false
			for _, r := range schema.Required {
				if r == "command" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("command should always be required, required fields: %v", schema.Required)
			}
		})
	}
}

func TestCreateCallKubectlTool_DescriptionContainsDefault(t *testing.T) {
	defaultID := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/mycluster"
	tool := createCallKubectlTool("readonly", defaultID, nil, "")

	schemaBytes, _ := json.Marshal(tool.InputSchema)
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(schemaBytes, &schema)

	prop, ok := schema.Properties["aks_resource_id"]
	if !ok {
		t.Fatal("aks_resource_id property not found in schema")
	}
	if prop.Description == "" {
		t.Error("expected non-empty description for aks_resource_id")
	}
}

func TestCreateCallKubectlTool_Name(t *testing.T) {
	tool := createCallKubectlTool("readonly", "", nil, "")
	if tool.Name != "call_kubectl" {
		t.Errorf("expected tool name call_kubectl, got %q", tool.Name)
	}
}

func TestRegisterKubectlTools_TokenAuthOnly(t *testing.T) {
	tools := RegisterKubectlTools("readonly", true, true, "", nil, "")
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool in tokenAuthOnly mode, got %d", len(tools))
	}
	if tools[0].Name != "call_kubectl" {
		t.Errorf("expected call_kubectl, got %q", tools[0].Name)
	}
}

func TestRegisterKubectlTools_TokenAuthOnly_WithDefault(t *testing.T) {
	defaultID := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/cluster"
	tools := RegisterKubectlTools("readonly", true, true, defaultID, nil, "")

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	schemaBytes, _ := json.Marshal(tools[0].InputSchema)
	var schema struct {
		Required []string `json:"required"`
	}
	_ = json.Unmarshal(schemaBytes, &schema)

	for _, r := range schema.Required {
		if r == "aks_resource_id" {
			t.Error("aks_resource_id should be optional when default resource ID is provided")
		}
	}
}

func TestCreateCallKubectlTool_AdvertisesConfiguredTargetAliases(t *testing.T) {
	tool := createCallKubectlTool("readonly", "", map[string]string{"prod": "id", "test": "id"}, "prod")
	schemaBytes, _ := json.Marshal(tool.InputSchema)
	var schema struct {
		Required   []string               `json:"required"`
		Properties map[string]interface{} `json:"properties"`
	}
	_ = json.Unmarshal(schemaBytes, &schema)
	if _, ok := schema.Properties["aks_target"]; !ok {
		t.Fatal("aks_target property not found in schema")
	}
	for _, required := range schema.Required {
		if required == "aks_resource_id" {
			t.Fatal("aks_resource_id must be optional when aliases are configured")
		}
	}
}

func TestCreateCallKubectlTool_TargetRequired_WhenAliasesHaveNoDefault(t *testing.T) {
	tool := createCallKubectlTool("readonly", "", map[string]string{"prod": "id", "test": "id"}, "")
	schemaBytes, _ := json.Marshal(tool.InputSchema)
	var schema struct {
		Required []string `json:"required"`
	}
	_ = json.Unmarshal(schemaBytes, &schema)

	found := false
	for _, required := range schema.Required {
		if required == "aks_target" {
			found = true
		}
		if required == "aks_resource_id" {
			t.Fatal("aks_resource_id must not be required when aliases are configured")
		}
	}
	if !found {
		t.Errorf("expected aks_target to be required when aliases are configured without a default, required fields: %v", schema.Required)
	}
}
