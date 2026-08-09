# AKS-MCP Triage Resources

This document describes the optional, bounded AKS triage resources exposed by
AKS-MCP. These resources are **disabled by default** and must be explicitly
opted in by the operator.

## Enablement

Triage resources are gated by an opt-in switch.

### Environment variable

```bash
export ENABLE_TRIAGE_RESOURCES=true
```

### Command-line flag

```bash
aks-mcp --enable-triage-resources
```

When the switch is off, the server does **not** advertise MCP resource
capabilities and `resources/list` returns an empty set. When the switch is on,
the server registers exactly four read-only `aks://cluster/*` resources and
advertises the capability truthfully.

## Resource contract

All resources are:

- **Read-only** - they never mutate cluster or Azure state.
- **Bounded** - every JSON payload is capped at 16 KiB. Oversized payloads are
  replaced with an error object.
- **Credential-free** - they never expose kubeconfig, Azure tokens, passwords,
  certificates, or logs.
- **Snapshot-only** - they return bounded configuration or explicitly stale
  placeholders, not raw Kubernetes objects.

| URI | MIME type | Description |
|-----|-----------|-------------|
| `aks://cluster/metadata` | `application/json` | Cluster metadata: `aks_resource_id`, schema version, and observation timestamp. |
| `aks://cluster/allowed-namespaces` | `application/json` | The configured allowed namespace list (`--allow-namespaces` / `AZURE_AKS_*`). |
| `aks://cluster/health-snapshot` | `application/json` | Placeholder for the latest retained health snapshot. Freshness is stated explicitly; call `aks_cluster_health` for a live observation. |
| `aks://cluster/policy-posture` | `application/json` | Placeholder for the latest retained policy posture. Freshness is stated explicitly; call `aks_policy_posture` for a live observation. |

All payloads use schema version `triage.v1` and include an `observed_at`
RFC3339 timestamp. The `health-snapshot` and `policy-posture` resources state
`freshness` clearly when no retained snapshot is available.

## Why opt-in?

Resource capability advertisement is disabled by default so that clients do not
waste tokens negotiating a resource surface they cannot use. The server only
advertises `resources/list` when the resources are actually registered.

## Helm chart

To enable triage resources when deploying with Helm, set:

```yaml
app:
  enableTriageResources: true
```

Then upgrade the release:

```bash
helm upgrade aks-mcp chart --set app.enableTriageResources=true
```

## Safety notes

- Do not enable this feature if you need to hide the default AKS resource ID or
  namespace allowlist from clients.
- Retained health snapshots and policy posture data are not collected by these
  resources; live observations remain in the dedicated triage tools.
