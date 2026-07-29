package remediation

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

type PlanRequest struct {
	ID             string `json:"id"`
	Cluster        string `json:"cluster"`
	Namespace      string `json:"namespace"`
	Pod            string `json:"pod"`
	IdempotencyKey string `json:"idempotency_key"`
	TTLMinutes     int    `json:"ttl_minutes"`
}
type ApprovalRequest struct {
	PlanID     string `json:"plan_id"`
	Digest     string `json:"digest"`
	ApprovedBy string `json:"approved_by"`
	TTLMinutes int    `json:"ttl_minutes"`
}
type VerifyRequest struct {
	PlanID string `json:"plan_id"`
}

func PlanTool() mcp.Tool {
	return mcp.NewTool("aks_plan_restart_failed_pod", mcp.WithDescription("Create an immutable, dry-run-only remediation plan for one failed pod. This does not mutate Kubernetes."), mcp.WithReadOnlyHintAnnotation(true), mcp.WithInputSchema[PlanRequest]())
}
func ApproveTool() mcp.Tool {
	return mcp.NewTool("aks_record_remediation_approval", mcp.WithDescription("Record an explicit external approval for an immutable remediation plan. Intended for an Argo approval gate, not a triage agent."), mcp.WithInputSchema[ApprovalRequest]())
}
func VerifyTool() mcp.Tool {
	return mcp.NewTool("aks_verify_remediation_plan", mcp.WithDescription("Return the audit evidence for a remediation plan; this does not mutate Kubernetes."), mcp.WithReadOnlyHintAnnotation(true), mcp.WithInputSchema[VerifyRequest]())
}

func PlanHandler(manager *Manager) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(_ context.Context, _ mcp.CallToolRequest, r PlanRequest) (Plan, error) {
		ttl := r.TTLMinutes
		if ttl <= 0 || ttl > 30 {
			ttl = 10
		}
		return manager.Create(Plan{ID: r.ID, Action: RestartFailedPod, Scope: Scope{Cluster: r.Cluster, Namespace: r.Namespace, Resource: "pod/" + r.Pod}, Preconditions: []string{"pod remains in failed state", "namespace remains allow-listed"}, DryRun: fmt.Sprintf("kubectl delete pod %s -n %s --dry-run=client", r.Pod, r.Namespace), Rollback: "The owning controller recreates the pod; no persistent object is changed.", IdempotencyKey: r.IdempotencyKey, ExpiresAt: manager.now().Add(time.Duration(ttl) * time.Minute)})
	})
}
func ApprovalHandler(manager *Manager) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(_ context.Context, _ mcp.CallToolRequest, r ApprovalRequest) (map[string]string, error) {
		ttl := r.TTLMinutes
		if ttl <= 0 || ttl > 30 {
			ttl = 10
		}
		if err := manager.Approve(r.PlanID, r.Digest, r.ApprovedBy, manager.now().Add(time.Duration(ttl)*time.Minute)); err != nil {
			return nil, err
		}
		return map[string]string{"status": "approved", "plan_id": r.PlanID}, nil
	})
}
func VerifyHandler(manager *Manager) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewStructuredToolHandler(func(_ context.Context, _ mcp.CallToolRequest, r VerifyRequest) ([]AuditEvent, error) {
		events := manager.Audit()
		out := make([]AuditEvent, 0)
		for _, e := range events {
			if e.PlanID == r.PlanID {
				out = append(out, e)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("plan not found")
		}
		return out, nil
	})
}
