# Durable MCP Tasks

`aks_cluster_health` can be run as an MCP Task only when AKS-MCP has durable
storage configured. This prevents a client receiving a task handle that is
lost when Kubernetes replaces the pod.

## Kubernetes deployment

Create a single-writer PersistentVolumeClaim, then enable the chart setting:

```yaml
taskStore:
  enabled: true
  existingClaim: aks-mcp-tasks
```

The chart mounts that claim at `/var/lib/aks-mcp/tasks` and passes it as
`--task-store-dir`. It intentionally refuses `taskStore.enabled=true` without
an existing claim. The bundled file store is for one replica; use a shared
`TaskStore` implementation before scaling replicas.

## Behaviour and safety

- Task records contain the MCP task state, owner session, a validated minimal
  cluster scope, bounded result, and expiry metadata. They never contain
  request headers, raw arguments or tokens.
- AKS-MCP supplies a 15-minute default TTL and rejects zero, negative or
  over-one-hour client TTLs before allocating a task handle.
- Completed results remain available after a process restart until their TTL
  expires.
- A task interrupted by a restart is marked `failed` with an explicit restart
  reason. AKS-MCP does not replay it: replay could duplicate a future mutation.
- `tasks/cancel` persists terminal cancellation; the server accepts at most
  four concurrent tasks.
- Without a configured store, task capability is not advertised and
  `aks_cluster_health` remains available synchronously.

The file-backed implementation comes from the temporary
`davidmarkgardiner/mcp-go` fork pending upstream acceptance of the generic
`TaskStore` hook.
