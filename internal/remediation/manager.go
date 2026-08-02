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
	store     Store
}

func NewManager() *Manager {
	return &Manager{plans: map[string]Plan{}, approvals: map[string]Approval{}, applied: map[string]time.Time{}, now: time.Now}
}

// NewManagerWithStore restores the shared durable plan/approval record before
// exposing any remediation operation. A corrupt or unreadable record fails
// closed rather than allowing an unapproved replacement state.
func NewManagerWithStore(store Store) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("remediation store is required")
	}
	state, err := store.Load()
	if err != nil {
		return nil, err
	}
	state.ensureMaps()
	manager := NewManager()
	manager.store = store
	manager.plans = state.Plans
	manager.approvals = state.Approvals
	for key, appliedAt := range state.Applied {
		manager.applied[key] = time.UnixMilli(appliedAt)
	}
	manager.audit = state.Audit
	return manager, nil
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
	if err := m.persistLocked(); err != nil {
		delete(m.plans, plan.ID)
		m.audit = m.audit[:len(m.audit)-1]
		return Plan{}, err
	}
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
	if err := m.persistLocked(); err != nil {
		delete(m.approvals, planID)
		m.audit = m.audit[:len(m.audit)-1]
		return err
	}
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
	// Reserve the idempotency key before releasing the lock or invoking kubectl.
	// A crash now fails closed rather than permitting two workers to apply the
	// same immutable plan concurrently.
	m.applied[plan.IdempotencyKey] = m.now()
	m.audit = append(m.audit, AuditEvent{At: m.now(), Type: "apply_attempt", PlanID: planID, Detail: identity})
	if err := m.persistLocked(); err != nil {
		delete(m.applied, plan.IdempotencyKey)
		m.audit = m.audit[:len(m.audit)-1]
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	if err := applier.Apply(ctx, plan); err != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.audit = append(m.audit, AuditEvent{At: m.now(), Type: "apply_failed", PlanID: planID, Detail: identity})
		if persistErr := m.persistLocked(); persistErr != nil {
			return persistErr
		}
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, AuditEvent{At: m.now(), Type: "applied", PlanID: planID, Detail: identity})
	return m.persistLocked()
}

func (m *Manager) persistLocked() error {
	if m.store == nil {
		return nil
	}
	applied := make(map[string]int64, len(m.applied))
	for key, value := range m.applied {
		applied[key] = value.UnixMilli()
	}
	return m.store.Save(State{Plans: m.plans, Approvals: m.approvals, Applied: applied, Audit: m.audit})
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
