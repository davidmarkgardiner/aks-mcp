// Package remediation implements the explicit plan -> approval -> apply ->
// verify boundary for AKS changes. It deliberately does not accept arbitrary
// kubectl text: every supported action has a typed scope and precondition.
package remediation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const RestartFailedPod = "restart_failed_pod"

type Scope struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Resource  string `json:"resource"`
}

type Plan struct {
	ID             string    `json:"id"`
	Action         string    `json:"action"`
	Scope          Scope     `json:"scope"`
	Preconditions  []string  `json:"preconditions"`
	DryRun         string    `json:"dry_run"`
	Rollback       string    `json:"rollback"`
	IdempotencyKey string    `json:"idempotency_key"`
	ExpiresAt      time.Time `json:"expires_at"`
	Digest         string    `json:"digest"`
}

type Approval struct {
	PlanID     string    `json:"plan_id"`
	Digest     string    `json:"digest"`
	ApprovedBy string    `json:"approved_by"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type AuditEvent struct {
	At     time.Time `json:"at"`
	Type   string    `json:"type"`
	PlanID string    `json:"plan_id"`
	Detail string    `json:"detail"`
}

// Applier must be bound to a restricted deployment identity, not the
// read-only identity used by triage agents.
type Applier interface {
	Apply(context.Context, Plan) error
}

type Manager struct {
	mu        sync.Mutex
	plans     map[string]Plan
	approvals map[string]Approval
	applied   map[string]time.Time
	audit     []AuditEvent
	now       func() time.Time
}

func NewManager() *Manager {
	return &Manager{plans: map[string]Plan{}, approvals: map[string]Approval{}, applied: map[string]time.Time{}, now: time.Now}
}

func (m *Manager) Create(plan Plan) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if plan.ID == "" || plan.IdempotencyKey == "" || plan.Scope.Cluster == "" || plan.Scope.Namespace == "" || plan.Scope.Resource == "" {
		return Plan{}, fmt.Errorf("plan requires id, scope and idempotency key")
	}
	if plan.Action != RestartFailedPod {
		return Plan{}, fmt.Errorf("unsupported remediation action %q", plan.Action)
	}
	if !plan.ExpiresAt.After(m.now()) {
		return Plan{}, fmt.Errorf("plan expiry must be in the future")
	}
	plan.Digest = digest(plan)
	m.plans[plan.ID] = plan
	m.audit = append(m.audit, AuditEvent{At: m.now(), Type: "plan_created", PlanID: plan.ID, Detail: plan.Digest})
	return plan, nil
}

func (m *Manager) Approve(planID, digest, approver string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	plan, ok := m.plans[planID]
	if !ok || plan.Digest != digest {
		return fmt.Errorf("plan not found or changed")
	}
	if approver == "" || !expiresAt.After(m.now()) || expiresAt.After(plan.ExpiresAt) {
		return fmt.Errorf("invalid approval")
	}
	m.approvals[planID] = Approval{PlanID: planID, Digest: digest, ApprovedBy: approver, ExpiresAt: expiresAt}
	m.audit = append(m.audit, AuditEvent{At: m.now(), Type: "plan_approved", PlanID: planID, Detail: approver})
	return nil
}

func (m *Manager) Apply(ctx context.Context, planID, digest, identity string, applier Applier) error {
	m.mu.Lock()
	plan, ok := m.plans[planID]
	approval, approved := m.approvals[planID]
	if !ok || plan.Digest != digest || !approved || approval.Digest != digest || !plan.ExpiresAt.After(m.now()) || !approval.ExpiresAt.After(m.now()) || identity == "" || applier == nil {
		m.mu.Unlock()
		return fmt.Errorf("remediation apply not authorized")
	}
	if _, replay := m.applied[plan.IdempotencyKey]; replay {
		m.mu.Unlock()
		return fmt.Errorf("remediation plan already applied")
	}
	m.audit = append(m.audit, AuditEvent{At: m.now(), Type: "apply_attempt", PlanID: planID, Detail: identity})
	m.mu.Unlock()
	if err := applier.Apply(ctx, plan); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applied[plan.IdempotencyKey] = m.now()
	m.audit = append(m.audit, AuditEvent{At: m.now(), Type: "applied", PlanID: planID, Detail: identity})
	return nil
}

func (m *Manager) Audit() []AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AuditEvent(nil), m.audit...)
}
func digest(p Plan) string {
	h := sha256.Sum256([]byte(p.ID + "|" + p.Action + "|" + p.Scope.Cluster + "|" + p.Scope.Namespace + "|" + p.Scope.Resource + "|" + p.IdempotencyKey + "|" + p.ExpiresAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:])
}
