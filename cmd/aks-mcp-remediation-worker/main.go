// aks-mcp-remediation-worker is intentionally separate from the public MCP
// server. An Argo workflow invokes it under a narrowly scoped service account
// after a human resumes the approval gate.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Azure/aks-mcp/internal/remediation"
)

func main() {
	var storeDir, planID, digest, identity, approvedBy string
	var approvalTTL int
	flag.StringVar(&storeDir, "store-dir", "", "PVC-backed remediation store directory")
	flag.StringVar(&planID, "plan-id", "", "immutable plan identifier")
	flag.StringVar(&digest, "digest", "", "approved immutable plan digest")
	flag.StringVar(&identity, "identity", os.Getenv("REMEDIATION_WORKER_IDENTITY"), "restricted worker identity")
	flag.StringVar(&approvedBy, "approved-by", "", "human approver identity; writes approval only")
	flag.IntVar(&approvalTTL, "approval-ttl-minutes", 10, "approval validity, 1-30 minutes")
	flag.Parse()
	if storeDir == "" || planID == "" || digest == "" {
		log.Fatal("--store-dir, --plan-id and --digest are required")
	}
	manager, err := remediation.NewManagerWithStore(remediation.NewFileStore(storeDir))
	if err != nil {
		log.Fatalf("load durable remediation record: %v", err)
	}
	if approvedBy != "" {
		if approvalTTL < 1 || approvalTTL > 30 {
			log.Fatal("approval TTL must be between 1 and 30 minutes")
		}
		if err := manager.Approve(planID, digest, approvedBy, time.Now().Add(time.Duration(approvalTTL)*time.Minute)); err != nil {
			log.Fatalf("record approval: %v", err)
		}
		fmt.Println("approval recorded")
		return
	}
	if identity == "" {
		log.Fatal("--identity or REMEDIATION_WORKER_IDENTITY is required for apply")
	}
	deleter, err := remediation.NewInClusterPodDeleter()
	if err != nil {
		log.Fatalf("create restricted in-cluster worker: %v", err)
	}
	if err := manager.Apply(context.Background(), planID, digest, identity, remediation.KubernetesApplier{Deleter: deleter}); err != nil {
		log.Fatalf("apply approved remediation: %v", err)
	}
	fmt.Println("approved remediation applied")
}
