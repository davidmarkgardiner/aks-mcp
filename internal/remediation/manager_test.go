package remediation

import (
	"context"
	"testing"
	"time"
)

type fakeApplier struct{ calls int }

func (f *fakeApplier) Apply(context.Context, Plan) error { f.calls++; return nil }
func plan(now time.Time) Plan {
	return Plan{ID: "p1", Action: RestartFailedPod, Scope: Scope{Cluster: "c", Namespace: "n", Resource: "pod/x"}, IdempotencyKey: "key", ExpiresAt: now.Add(time.Hour)}
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
