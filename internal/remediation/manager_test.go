package remediation

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeApplier struct{ calls int }

func (f *fakeApplier) Apply(context.Context, Plan) error { f.calls++; return nil }
func plan(now time.Time) Plan {
	return Plan{ID: "p1", Action: RestartFailedPod, Scope: Scope{Cluster: "c", Namespace: "n", Resource: "pod/x"}, IdempotencyKey: "key", ExpiresAt: now.Add(time.Hour)}
}

func TestFailedApplyReservesIdempotencyKeyAndCannotReplay(t *testing.T) {
	now := time.Now()
	m := NewManager()
	m.now = func() time.Time { return now }
	p, err := m.Create(plan(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Approve(p.ID, p.Digest, "argo", now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	failing := PodDeleterFunc(func(context.Context, string, string) error { return errors.New("api unavailable") })
	if err := m.Apply(context.Background(), p.ID, p.Digest, "restricted", KubernetesApplier{Deleter: failing}); err == nil {
		t.Fatal("failed worker was reported as success")
	}
	if err := m.Apply(context.Background(), p.ID, p.Digest, "restricted", &fakeApplier{}); err == nil {
		t.Fatal("a failed apply could be replayed")
	}
}

func TestDurableApprovalRecordSurvivesManagerRestart(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	store := NewFileStore(t.TempDir())
	m, err := NewManagerWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return now }
	p, err := m.Create(plan(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Approve(p.ID, p.Digest, "argo-approval", now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManagerWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now }
	a := &fakeApplier{}
	if err := restarted.Apply(context.Background(), p.ID, p.Digest, "restricted-worker", a); err != nil {
		t.Fatalf("durable approval was not applied: %v", err)
	}
	if a.calls != 1 {
		t.Fatalf("calls=%d", a.calls)
	}
}

func TestRestrictedKubernetesApplierUsesOnlyApprovedPodScope(t *testing.T) {
	var gotNamespace, gotPod string
	applier := KubernetesApplier{Deleter: PodDeleterFunc(func(_ context.Context, namespace, pod string) error {
		gotNamespace, gotPod = namespace, pod
		return nil
	})}
	if err := applier.Apply(context.Background(), Plan{Action: RestartFailedPod, Scope: Scope{Namespace: "team-a", Resource: "pod/api-123"}}); err != nil {
		t.Fatal(err)
	}
	if gotNamespace != "team-a" || gotPod != "api-123" {
		t.Fatalf("unexpected Kubernetes delete scope: namespace=%s pod=%s", gotNamespace, gotPod)
	}
	if err := applier.Apply(context.Background(), Plan{Action: RestartFailedPod, Scope: Scope{Namespace: "team-a", Resource: "pod/api;rm"}}); err == nil {
		t.Fatal("unsafe resource was accepted")
	}
}
func TestApprovalBoundaryRejectsUnapprovedExpiredChangedAndReplay(t *testing.T) {
	now := time.Now()
	m := NewManager()
	m.now = func() time.Time { return now }
	p, err := m.Create(plan(now))
	if err != nil {
		t.Fatal(err)
	}
	a := &fakeApplier{}
	if err := m.Apply(context.Background(), p.ID, p.Digest, "restricted", a); err == nil {
		t.Fatal("unapproved apply accepted")
	}
	if err := m.Approve(p.ID, p.Digest, "argo", now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(context.Background(), p.ID, "changed", "restricted", a); err == nil {
		t.Fatal("changed plan accepted")
	}
	if err := m.Apply(context.Background(), p.ID, p.Digest, "restricted", a); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(context.Background(), p.ID, p.Digest, "restricted", a); err == nil {
		t.Fatal("replay accepted")
	}
	if a.calls != 1 {
		t.Fatalf("calls=%d", a.calls)
	}
}
