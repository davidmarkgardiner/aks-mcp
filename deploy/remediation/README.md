# Approval-gated remediation worker

This is the mutation boundary for issue #5. The public AKS-MCP server exposes
only plan and verify tools. It does not expose approval or apply.

Before applying `argo-approval-apply.yaml`, replace all `REMEDIATION_NAMESPACE`
and `REPLACE_*` values. The worker Role is deliberately namespaced and grants
only `get`/`delete` on Pods. Create one deployment per approved namespace; do
not widen it into a cluster role.

The WorkflowTemplate has three stages:

1. `human-approval` suspends the workflow.
2. A human with Argo access resumes it and the workflow records the supplied
   immutable plan id/digest approval on the shared PVC.
3. The separately identified worker reloads that durable record and applies
   only the typed, approved pod restart. A changed, expired, unapproved or
   replayed plan fails in the worker before `kubectl` runs.

The production integration must bind `approved-by` to the authenticated Argo
resume actor (or an equivalent signed approval system). The parameter is not a
substitute for identity verification. No manifest here is applied by tests.
