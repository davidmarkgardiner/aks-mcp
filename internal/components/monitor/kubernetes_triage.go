package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Azure/aks-mcp/internal/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// KubernetesJSONReader is deliberately narrow: every triage query is a
// controller-owned, read-only kubectl invocation.  It makes the projection
// logic fixture-testable without a cluster and prevents callers from passing a
// command string through the MCP contract.
type KubernetesJSONReader func(context.Context, ...string) ([]byte, error)

const (
	maxKubernetesTriageFindings = 50
	kubernetesTriageSchema      = "triage.v1"
)

var errKubernetesPermissionDenied = errors.New("Kubernetes permission denied")

// KubernetesTriageRequest bounds namespaced diagnostic queries.  An empty
// namespace is permitted only when the server itself has no namespace allow
// list; it means the configured Kubernetes context, not an arbitrary context
// supplied by the caller.
type KubernetesTriageRequest struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"Optional Kubernetes namespace, subject to server allow-list"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum findings (1-50, default 50)"`
}

// KubernetesTriageScope and KubernetesTriageFinding are the common safe
// envelope used by each distinct signal contract below.  They contain neither
// kubeconfig material nor raw pod specs, environment variables or log output.
type KubernetesTriageScope struct {
	Cluster        string `json:"cluster"`
	Namespace      string `json:"namespace,omitempty"`
	NamespaceScope string `json:"namespace_scope"`
}

type KubernetesTriageFinding struct {
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Reason     string `json:"reason"`
	Severity   string `json:"severity"`
	ObservedAt string `json:"observed_at"`
}

type NodePressureResult struct {
	SchemaVersion string                    `json:"schema_version"`
	Signal        string                    `json:"signal"`
	Scope         KubernetesTriageScope     `json:"scope"`
	ObservedAt    string                    `json:"observed_at"`
	Findings      []KubernetesTriageFinding `json:"findings"`
	Truncated     bool                      `json:"truncated"`
}

type WorkloadFailureResult struct {
	SchemaVersion string                    `json:"schema_version"`
	Signal        string                    `json:"signal"`
	Scope         KubernetesTriageScope     `json:"scope"`
	ObservedAt    string                    `json:"observed_at"`
	Findings      []KubernetesTriageFinding `json:"findings"`
	Truncated     bool                      `json:"truncated"`
}

type PolicyPostureResult struct {
	SchemaVersion string                    `json:"schema_version"`
	Signal        string                    `json:"signal"`
	Scope         KubernetesTriageScope     `json:"scope"`
	ObservedAt    string                    `json:"observed_at"`
	Findings      []KubernetesTriageFinding `json:"findings"`
	Truncated     bool                      `json:"truncated"`
}

type DeploymentHistoryResult struct {
	SchemaVersion string                    `json:"schema_version"`
	Signal        string                    `json:"signal"`
	Scope         KubernetesTriageScope     `json:"scope"`
	ObservedAt    string                    `json:"observed_at"`
	Findings      []KubernetesTriageFinding `json:"findings"`
	Truncated     bool                      `json:"truncated"`
}

func RegisterNodePressure() mcp.Tool {
	return mcp.NewTool("aks_node_pressure",
		mcp.WithDescription("Return bounded, structured, read-only Kubernetes node pressure findings."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithInputSchema[KubernetesTriageRequest](),
		mcp.WithOutputSchema[NodePressureResult](),
	)
}

func RegisterWorkloadFailures() mcp.Tool {
	return mcp.NewTool("aks_workload_failures",
		mcp.WithDescription("Return bounded, structured, read-only Kubernetes workload failure findings."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithInputSchema[KubernetesTriageRequest](),
		mcp.WithOutputSchema[WorkloadFailureResult](),
	)
}

func RegisterPolicyPosture() mcp.Tool {
	return mcp.NewTool("aks_policy_posture",
		mcp.WithDescription("Return bounded, structured, read-only Kubernetes policy report findings."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithInputSchema[KubernetesTriageRequest](),
		mcp.WithOutputSchema[PolicyPostureResult](),
	)
}

func RegisterDeploymentHistory() mcp.Tool {
	return mcp.NewTool("aks_deployment_history",
		mcp.WithDescription("Return bounded, structured, read-only Kubernetes deployment rollout findings."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithInputSchema[KubernetesTriageRequest](),
		mcp.WithOutputSchema[DeploymentHistoryResult](),
	)
}

func GetNodePressureHandler(cfg *config.ConfigData) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return GetNodePressureHandlerWithReader(cfg, readOnlyKubectlJSON)
}

func GetNodePressureHandlerWithReader(cfg *config.ConfigData, read KubernetesJSONReader) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, request KubernetesTriageRequest) (NodePressureResult, error) {
		if err := validateKubernetesTriageRequest(request, cfg, false); err != nil {
			return NodePressureResult{}, err
		}
		raw, err := read(ctx, "get", "nodes", "-o", "json")
		if err != nil {
			return NodePressureResult{}, safeKubernetesQueryError("node pressure", err)
		}
		return newNodePressureResult(request, cfg, raw, time.Now().UTC())
	})
}

func GetWorkloadFailuresHandler(cfg *config.ConfigData) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return GetWorkloadFailuresHandlerWithReader(cfg, readOnlyKubectlJSON)
}

func GetWorkloadFailuresHandlerWithReader(cfg *config.ConfigData, read KubernetesJSONReader) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, request KubernetesTriageRequest) (WorkloadFailureResult, error) {
		if err := validateKubernetesTriageRequest(request, cfg, true); err != nil {
			return WorkloadFailureResult{}, err
		}
		raw, err := readNamespacedJSON(ctx, request, "pods", read)
		if err != nil {
			return WorkloadFailureResult{}, safeKubernetesQueryError("workload failures", err)
		}
		return newWorkloadFailureResult(request, cfg, raw, time.Now().UTC())
	})
}

func GetPolicyPostureHandler(cfg *config.ConfigData) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return GetPolicyPostureHandlerWithReader(cfg, readOnlyKubectlJSON)
}

func GetPolicyPostureHandlerWithReader(cfg *config.ConfigData, read KubernetesJSONReader) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, request KubernetesTriageRequest) (PolicyPostureResult, error) {
		if err := validateKubernetesTriageRequest(request, cfg, true); err != nil {
			return PolicyPostureResult{}, err
		}
		raw, err := readNamespacedJSON(ctx, request, "policyreports.wgpolicyk8s.io", read)
		if err != nil {
			return PolicyPostureResult{}, safeKubernetesQueryError("policy posture", err)
		}
		return newPolicyPostureResult(request, cfg, raw, time.Now().UTC())
	})
}

func GetDeploymentHistoryHandler(cfg *config.ConfigData) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return GetDeploymentHistoryHandlerWithReader(cfg, readOnlyKubectlJSON)
}

func GetDeploymentHistoryHandlerWithReader(cfg *config.ConfigData, read KubernetesJSONReader) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, request KubernetesTriageRequest) (DeploymentHistoryResult, error) {
		if err := validateKubernetesTriageRequest(request, cfg, true); err != nil {
			return DeploymentHistoryResult{}, err
		}
		raw, err := readNamespacedJSON(ctx, request, "deployments", read)
		if err != nil {
			return DeploymentHistoryResult{}, safeKubernetesQueryError("deployment history", err)
		}
		return newDeploymentHistoryResult(request, cfg, raw, time.Now().UTC())
	})
}

func readOnlyKubectlJSON(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "kubectl", args...)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			stderr := strings.ToLower(string(exitError.Stderr))
			if strings.Contains(stderr, "forbidden") || strings.Contains(stderr, "unauthorized") {
				return nil, errKubernetesPermissionDenied
			}
		}
		return nil, errors.New("read-only kubectl query failed")
	}
	return output, nil
}

func readNamespacedJSON(ctx context.Context, request KubernetesTriageRequest, resource string, read KubernetesJSONReader) ([]byte, error) {
	args := []string{"get", resource}
	if request.Namespace == "" {
		args = append(args, "--all-namespaces")
	} else {
		args = append(args, "--namespace", request.Namespace)
	}
	return read(ctx, append(args, "-o", "json")...)
}

func validateKubernetesTriageRequest(request KubernetesTriageRequest, cfg *config.ConfigData, namespaced bool) error {
	if request.Limit < 0 || request.Limit > maxKubernetesTriageFindings {
		return fmt.Errorf("invalid limit: must be between 1 and %d", maxKubernetesTriageFindings)
	}
	if request.Namespace != "" && !safeKubernetesNamespace(request.Namespace) {
		return fmt.Errorf("invalid namespace")
	}
	if !namespaced && request.Namespace != "" {
		return fmt.Errorf("namespace is not valid for this cluster-scoped query")
	}
	if !namespaced {
		return nil
	}
	allowed := splitAllowedNamespaces(cfg.AllowNamespaces)
	if len(allowed) == 0 {
		return nil
	}
	if request.Namespace == "" {
		return fmt.Errorf("namespace is required by the server allow-list")
	}
	for _, namespace := range allowed {
		if request.Namespace == namespace {
			return nil
		}
	}
	return fmt.Errorf("namespace is not permitted by the server allow-list")
}

func safeKubernetesNamespace(value string) bool {
	if len(value) > 63 || value == "" || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func splitAllowedNamespaces(value string) []string {
	var namespaces []string
	for _, namespace := range strings.Split(value, ",") {
		if namespace = strings.TrimSpace(namespace); namespace != "" {
			namespaces = append(namespaces, namespace)
		}
	}
	return namespaces
}

func triageLimit(limit int) int {
	if limit == 0 {
		return maxKubernetesTriageFindings
	}
	return limit
}

func triageScope(request KubernetesTriageRequest, cfg *config.ConfigData) KubernetesTriageScope {
	cluster := cfg.DefaultAKSResourceID
	if cluster == "" {
		cluster = "configured-kubernetes-context"
	}
	namespaceScope := "all-permitted-namespaces"
	if request.Namespace != "" {
		namespaceScope = "single-namespace"
	}
	return KubernetesTriageScope{Cluster: cluster, Namespace: request.Namespace, NamespaceScope: namespaceScope}
}

func boundedFindings(findings []KubernetesTriageFinding, limit int) ([]KubernetesTriageFinding, bool) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Namespace == findings[j].Namespace {
			return findings[i].Name < findings[j].Name
		}
		return findings[i].Namespace < findings[j].Namespace
	})
	if len(findings) <= limit {
		return findings, false
	}
	return findings[:limit], true
}

func safeKubernetesQueryError(signal string, err error) error {
	if errors.Is(err, errKubernetesPermissionDenied) || strings.Contains(strings.ToLower(err.Error()), "forbidden") || strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		return fmt.Errorf("permission denied reading %s", signal)
	}
	return fmt.Errorf("read-only Kubernetes query failed for %s", signal)
}

type kubernetesList struct {
	Items []json.RawMessage `json:"items"`
}

func newNodePressureResult(request KubernetesTriageRequest, cfg *config.ConfigData, raw []byte, observedAt time.Time) (NodePressureResult, error) {
	var list kubernetesList
	if err := json.Unmarshal(raw, &list); err != nil {
		return NodePressureResult{}, fmt.Errorf("invalid Kubernetes node response")
	}
	findings := []KubernetesTriageFinding{}
	for _, item := range list.Items {
		var node struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		}
		if err := json.Unmarshal(item, &node); err != nil {
			return NodePressureResult{}, fmt.Errorf("invalid Kubernetes node response")
		}
		for _, condition := range node.Status.Conditions {
			if condition.Status == "True" && (condition.Type == "MemoryPressure" || condition.Type == "DiskPressure" || condition.Type == "PIDPressure") {
				findings = append(findings, KubernetesTriageFinding{Name: node.Metadata.Name, Reason: condition.Type, Severity: "warning", ObservedAt: observedAt.Format(time.RFC3339)})
			}
		}
	}
	findings, truncated := boundedFindings(findings, triageLimit(request.Limit))
	return NodePressureResult{SchemaVersion: kubernetesTriageSchema, Signal: "kubernetes_node_pressure", Scope: triageScope(request, cfg), ObservedAt: observedAt.Format(time.RFC3339), Findings: findings, Truncated: truncated}, nil
}

func newWorkloadFailureResult(request KubernetesTriageRequest, cfg *config.ConfigData, raw []byte, observedAt time.Time) (WorkloadFailureResult, error) {
	var list kubernetesList
	if err := json.Unmarshal(raw, &list); err != nil {
		return WorkloadFailureResult{}, fmt.Errorf("invalid Kubernetes workload response")
	}
	findings := []KubernetesTriageFinding{}
	for _, item := range list.Items {
		var pod struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				Reason            string `json:"reason"`
				ContainerStatuses []struct {
					State struct {
						Waiting *struct {
							Reason string `json:"reason"`
						} `json:"waiting"`
						Terminated *struct {
							Reason string `json:"reason"`
						} `json:"terminated"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		}
		if err := json.Unmarshal(item, &pod); err != nil {
			return WorkloadFailureResult{}, fmt.Errorf("invalid Kubernetes workload response")
		}
		reason := pod.Status.Reason
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
				reason = status.State.Waiting.Reason
			}
			if status.State.Terminated != nil && status.State.Terminated.Reason != "" {
				reason = status.State.Terminated.Reason
			}
		}
		if reason == "" && pod.Status.Phase != "Failed" {
			continue
		}
		if reason == "" {
			reason = pod.Status.Phase
		}
		findings = append(findings, KubernetesTriageFinding{Namespace: pod.Metadata.Namespace, Name: pod.Metadata.Name, Reason: reason, Severity: "warning", ObservedAt: observedAt.Format(time.RFC3339)})
	}
	findings, truncated := boundedFindings(findings, triageLimit(request.Limit))
	return WorkloadFailureResult{SchemaVersion: kubernetesTriageSchema, Signal: "kubernetes_workload_failure", Scope: triageScope(request, cfg), ObservedAt: observedAt.Format(time.RFC3339), Findings: findings, Truncated: truncated}, nil
}

func newPolicyPostureResult(request KubernetesTriageRequest, cfg *config.ConfigData, raw []byte, observedAt time.Time) (PolicyPostureResult, error) {
	var list kubernetesList
	if err := json.Unmarshal(raw, &list); err != nil {
		return PolicyPostureResult{}, fmt.Errorf("invalid Kubernetes policy response")
	}
	findings := []KubernetesTriageFinding{}
	for _, item := range list.Items {
		var report struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Summary struct {
				Fail int `json:"fail"`
				Warn int `json:"warn"`
			} `json:"summary"`
		}
		if err := json.Unmarshal(item, &report); err != nil {
			return PolicyPostureResult{}, fmt.Errorf("invalid Kubernetes policy response")
		}
		if report.Summary.Fail > 0 {
			findings = append(findings, KubernetesTriageFinding{Namespace: report.Metadata.Namespace, Name: report.Metadata.Name, Reason: fmt.Sprintf("%d policy failures", report.Summary.Fail), Severity: "error", ObservedAt: observedAt.Format(time.RFC3339)})
		} else if report.Summary.Warn > 0 {
			findings = append(findings, KubernetesTriageFinding{Namespace: report.Metadata.Namespace, Name: report.Metadata.Name, Reason: fmt.Sprintf("%d policy warnings", report.Summary.Warn), Severity: "warning", ObservedAt: observedAt.Format(time.RFC3339)})
		}
	}
	findings, truncated := boundedFindings(findings, triageLimit(request.Limit))
	return PolicyPostureResult{SchemaVersion: kubernetesTriageSchema, Signal: "kubernetes_policy_posture", Scope: triageScope(request, cfg), ObservedAt: observedAt.Format(time.RFC3339), Findings: findings, Truncated: truncated}, nil
}

func newDeploymentHistoryResult(request KubernetesTriageRequest, cfg *config.ConfigData, raw []byte, observedAt time.Time) (DeploymentHistoryResult, error) {
	var list kubernetesList
	if err := json.Unmarshal(raw, &list); err != nil {
		return DeploymentHistoryResult{}, fmt.Errorf("invalid Kubernetes deployment response")
	}
	findings := []KubernetesTriageFinding{}
	for _, item := range list.Items {
		var deployment struct {
			Metadata struct {
				Name        string            `json:"name"`
				Namespace   string            `json:"namespace"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Status struct {
				UnavailableReplicas int `json:"unavailableReplicas"`
			} `json:"status"`
		}
		if err := json.Unmarshal(item, &deployment); err != nil {
			return DeploymentHistoryResult{}, fmt.Errorf("invalid Kubernetes deployment response")
		}
		if deployment.Status.UnavailableReplicas == 0 {
			continue
		}
		revision := deployment.Metadata.Annotations["deployment.kubernetes.io/revision"]
		if revision == "" {
			revision = "unknown"
		}
		findings = append(findings, KubernetesTriageFinding{Namespace: deployment.Metadata.Namespace, Name: deployment.Metadata.Name, Reason: fmt.Sprintf("revision %s has %d unavailable replicas", revision, deployment.Status.UnavailableReplicas), Severity: "warning", ObservedAt: observedAt.Format(time.RFC3339)})
	}
	findings, truncated := boundedFindings(findings, triageLimit(request.Limit))
	return DeploymentHistoryResult{SchemaVersion: kubernetesTriageSchema, Signal: "kubernetes_deployment_history", Scope: triageScope(request, cfg), ObservedAt: observedAt.Format(time.RFC3339), Findings: findings, Truncated: truncated}, nil
}
