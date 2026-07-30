package config

import (
	"strings"
	"testing"
)

const testAKSID = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/cluster"

func TestParseAKSTargetsRejectsInvalidAndDuplicateEntries(t *testing.T) {
	for _, raw := range []string{"bad=/not-an-aks-id", "prod=" + testAKSID + ",prod=" + testAKSID, "not valid=" + testAKSID} {
		if _, err := parseAKSTargets(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestResolveAKSResourceIDUsesAliasAndPreservesRawID(t *testing.T) {
	cfg := &ConfigData{AKSTargets: map[string]string{"prod": testAKSID}, DefaultAKSTarget: "prod"}
	resolved, err := cfg.ResolveAKSResourceID(map[string]interface{}{"aks_target": "prod"})
	if err != nil || resolved["aks_resource_id"] != testAKSID {
		t.Fatalf("expected alias to resolve, got %#v, %v", resolved, err)
	}
	raw := testAKSID + "-raw"
	if _, err := cfg.ResolveAKSResourceID(map[string]interface{}{"aks_target": "missing", "aks_resource_id": raw}); err == nil {
		t.Fatal("expected unknown alias to fail even when a raw ID is supplied")
	}
	if _, err := cfg.ResolveAKSResourceID(map[string]interface{}{"aks_target": "missing"}); err == nil {
		t.Fatal("expected unknown alias to fail before execution")
	}
	if _, err := cfg.ResolveAKSResourceID(map[string]interface{}{"aks_resource_id": raw}); err == nil {
		t.Fatal("expected non-allowlisted raw resource ID to be rejected")
	}
	resolved, err = cfg.ResolveAKSResourceID(map[string]interface{}{"aks_resource_id": strings.ToUpper(testAKSID)})
	if err != nil || !strings.EqualFold(resolved["aks_resource_id"].(string), testAKSID) {
		t.Fatalf("expected allowlisted raw ID to be accepted, got %#v, %v", resolved, err)
	}
	resolved, err = cfg.ResolveAKSResourceID(map[string]interface{}{})
	if err != nil || resolved["aks_resource_id"] != testAKSID {
		t.Fatalf("expected default target to resolve, got %#v, %v", resolved, err)
	}
}

func TestResolveAKSResourceIDRequiresTargetWhenNoDefault(t *testing.T) {
	cfg := &ConfigData{AKSTargets: map[string]string{"prod": testAKSID}}
	if _, err := cfg.ResolveAKSResourceID(map[string]interface{}{}); err == nil {
		t.Fatal("expected an error when targets are configured but nothing selects one")
	}
}

func TestResolveAKSResourceIDWithoutTargetsKeepsRawIDBehavior(t *testing.T) {
	cfg := &ConfigData{AKSTargets: map[string]string{}, DefaultAKSResourceID: testAKSID}
	raw := testAKSID + "-raw"
	resolved, err := cfg.ResolveAKSResourceID(map[string]interface{}{"aks_resource_id": raw})
	if err != nil || resolved["aks_resource_id"] != raw {
		t.Fatalf("expected raw ID to be preserved without targets, got %#v, %v", resolved, err)
	}
	resolved, err = cfg.ResolveAKSResourceID(map[string]interface{}{})
	if err != nil || resolved["aks_resource_id"] != testAKSID {
		t.Fatalf("expected default resource ID fallback, got %#v, %v", resolved, err)
	}
}
