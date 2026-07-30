package k8s

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Azure/mcp-kubernetes/pkg/kubectl"
	"github.com/Azure/mcp-kubernetes/pkg/security"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	AccessLevelReadOnly  = "readonly"
	AccessLevelReadWrite = "readwrite"
)

func createCallKubectlTool(accessLevel string, defaultAKSResourceID string, aksTargets map[string]string, defaultAKSTarget string) mcp.Tool {
	var description string

	readCommands := strings.Join(security.KubectlReadOperations, ", ")
	writeCommands := strings.Join(security.KubectlReadWriteOperations, ", ")

	switch accessLevel {
	case AccessLevelReadOnly:
		description = fmt.Sprintf(`Execute kubectl commands with read-only access.

Pass full kubectl command including 'kubectl' prefix. All standard kubectl flags are supported.

Allowed commands:
%s

Examples:
- command='kubectl get pods -n default'
- command='kubectl describe deployment myapp -n production'
- command='kubectl logs nginx-pod -f'
- command='kubectl top pods'
- command='kubectl events --all-namespaces'
- command='kubectl explain pods.spec.containers'
- command='kubectl auth can-i create pods'`, readCommands)
	case AccessLevelReadWrite:
		description = fmt.Sprintf(`Execute kubectl commands with read and write access.

Pass full kubectl command including 'kubectl' prefix. All standard kubectl flags are supported.

Allowed commands:
Read: %s
Write: %s

Examples:
- command='kubectl get pods -n default'
- command='kubectl create -f deployment.yaml'
- command='kubectl apply -f deployment.yaml'
- command='kubectl delete pod nginx-pod'
- command='kubectl scale deployment myapp --replicas=3'
- command='kubectl rollout status deployment/myapp'
- command='kubectl label pods foo unhealthy=true'
- command='kubectl exec nginx-pod -- date'
- command='kubectl config use-context my-cluster-context'`, readCommands, writeCommands)
	default:
		description = fmt.Sprintf(`Execute kubectl commands with unknown access level (defaulting to read-only).

Pass full kubectl command including 'kubectl' prefix. All standard kubectl flags are supported.

Allowed commands:
%s

Examples:
- command='kubectl get pods -n default'
- command='kubectl describe deployment myapp -n production'
- command='kubectl logs nginx-pod -f'`, readCommands)
	}

	resourceIDDesc := "Full Azure Resource ID of the AKS cluster (e.g., /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.ContainerService/managedClusters/{clusterName})"
	if defaultAKSTarget != "" {
		resourceIDDesc = fmt.Sprintf("Full Azure Resource ID of the AKS cluster. Defaults to configured target alias %q if neither target nor resource ID is provided.", defaultAKSTarget)
	} else if defaultAKSResourceID != "" {
		resourceIDDesc = fmt.Sprintf("Full Azure Resource ID of the AKS cluster. Defaults to %s if not provided.", defaultAKSResourceID)
	}

	resourceIDOpts := []mcp.PropertyOption{mcp.Description(resourceIDDesc)}
	if defaultAKSResourceID == "" && len(aksTargets) == 0 {
		resourceIDOpts = append(resourceIDOpts, mcp.Required())
	}
	targetOpts := []mcp.PropertyOption{mcp.Description("Configured server-side AKS target alias. Aliases are allowlisted and resolve to a resource ID before Azure is called.")}
	if len(aksTargets) > 0 {
		aliases := make([]string, 0, len(aksTargets))
		for alias := range aksTargets {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		targetOpts = append(targetOpts, mcp.Enum(aliases...))
	}

	return mcp.NewTool("call_kubectl",
		mcp.WithDescription(description),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("Full kubectl command to execute (e.g., 'kubectl get pods -n default', 'kubectl describe deployment myapp', 'kubectl logs nginx-pod -f')"),
		),
		mcp.WithString("aks_resource_id", resourceIDOpts...),
		mcp.WithString("aks_target", targetOpts...),
	)
}

func RegisterKubectlTools(accessLevel string, useUnifiedTool bool, tokenAuthOnly bool, defaultAKSResourceID string, aksTargets map[string]string, defaultAKSTarget string) []mcp.Tool {
	if tokenAuthOnly {
		return []mcp.Tool{
			createCallKubectlTool(accessLevel, defaultAKSResourceID, aksTargets, defaultAKSTarget),
		}
	}

	return kubectl.RegisterKubectlTools(accessLevel, useUnifiedTool)
}
