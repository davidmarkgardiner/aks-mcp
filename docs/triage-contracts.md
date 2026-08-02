# AKS triage contracts (`triage.v1`)

The read-only AKS triage tools return structured JSON so an MCP or kagent
specialist can classify evidence without parsing prose.  Every result contains:

- `schema_version`: currently `triage.v1`;
- `signal`: the stable source-specific signal name;
- `scope`: the configured cluster identity and the permitted namespace scope;
- `observed_at`: the RFC3339 time at which the server made the observation;
- `findings`: a bounded list of safe projections; and
- `truncated`: true when more findings existed than the requested safe limit.

The server never returns raw kubeconfig material, pod specs, environment
variables, logs, or credentials in these contracts.  Empty findings are a
successful observation, not an error.  Permission failures are returned as a
safe MCP tool error and do not echo command output.

| Tool | Signal | Read-only source | Finding meaning |
| --- | --- | --- | --- |
| `aks_cluster_health` | `azure_resource_health_activity_log` | Azure Resource Health activity log | AKS platform health events |
| `aks_node_pressure` | `kubernetes_node_pressure` | `kubectl get nodes -o json` | `MemoryPressure`, `DiskPressure`, or `PIDPressure` conditions |
| `aks_workload_failures` | `kubernetes_workload_failure` | bounded `kubectl get pods ... -o json` | failed phase or a container waiting/terminated reason |
| `aks_policy_posture` | `kubernetes_policy_posture` | bounded `kubectl get policyreports.wgpolicyk8s.io ... -o json` | policy report fail/warn summary |
| `aks_deployment_history` | `kubernetes_deployment_history` | bounded `kubectl get deployments ... -o json` | current rollout revision with unavailable replicas |

Namespaced tools honour the server `--allow-namespaces` policy.  If that policy
is set, callers must name one permitted namespace; they cannot expand the
query to all namespaces.  `limit` is 1–50 (default 50).  The server owns all
kubectl arguments; the MCP client cannot inject a command or a context.

The fixture at `docs/fixtures/triage-v1-workload-failure.json` is deliberately
machine-readable and contains the same shape as a live result.  A specialist
can branch on `schema_version`, `signal`, `findings[*].severity`, and
`findings[*].reason` without interpreting narrative text.
