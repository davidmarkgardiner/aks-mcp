package config

import "testing"

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
	resolved, err = cfg.ResolveAKSResourceID(map[string]interface{}{"aks_target": "missing", "aks_resource_id": raw})
	if err != nil || resolved["aks_resource_id"] != raw {
		t.Fatalf("expected explicit raw ID to win, got %#v, %v", resolved, err)
	}
	if _, err := cfg.ResolveAKSResourceID(map[string]interface{}{"aks_target": "missing"}); err == nil {
		t.Fatal("expected unknown alias to fail before execution")
	}
}
