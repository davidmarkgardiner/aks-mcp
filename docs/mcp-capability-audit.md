# AKS-MCP capability audit — MCP 2025-06-18

**Audit basis:** source snapshot `8e84bce`, inspected 2026-07-29. This is a
static implementation audit: it does not contact Azure, start a server, or
change credentials or infrastructure. **Supported** means the relevant
feature is explicitly configured and has an AKS-MCP implementation path;
**Partial** means that only part of the feature, or only SDK-provided behavior,
is evidenced; **Missing** means that this worktree has no registration or use
of the feature. These are implementation findings, not a wire-level
conformance certification.

## Specification baseline

The [MCP 2025-06-18 specification](https://modelcontextprotocol.io/specification/2025-06-18)
requires JSON-RPC 2.0 plus lifecycle management for every implementation, and
makes server features optional. During initialization a server returns its
negotiated version and advertised capabilities; it must then use only
negotiated capabilities ([lifecycle](https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle)).
The standard transports are stdio and Streamable HTTP; the latter supersedes
the former HTTP+SSE transport and requires Origin validation
([transports](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)).

## Capability matrix

| MCP 2025-06-18 area | Status | AKS-MCP evidence and assessment |
| --- | --- | --- |
| JSON-RPC, initialization, version/capability negotiation | Partial | The server is built on `github.com/mark3labs/mcp-go v0.54.1` ([go.mod](../go.mod)); the E2E client initializes using `mcp.LATEST_PROTOCOL_VERSION` ([test/e2e/pkg/client/mcp_client.go](../test/e2e/pkg/client/mcp_client.go)). This provides a clear SDK path, but this worktree contains no wire assertion that an AKS-MCP server returns `2025-06-18` and enforces the negotiated version/header. |
| stdio transport | Supported | `Run` dispatches `stdio` to `server.ServeStdio` ([internal/server/server.go](../internal/server/server.go)); this matches the standard newline-delimited stdio transport. |
| Streamable HTTP transport and session endpoint | Supported | `Run` constructs `NewStreamableHTTPServer` and mounts it at `/mcp` ([internal/server/server.go](../internal/server/server.go)). The endpoint description explicitly supports POST, GET, and DELETE session operations. |
| Streamable HTTP Origin/binding/authentication safeguards | Partial | Default host is `127.0.0.1`, and explicit host/origin controls are configured ([internal/config/config.go](../internal/config/config.go)); the MCP route is wrapped in the HTTP security middleware ([internal/server/server.go](../internal/server/server.go)). Handler and middleware tests cover Host/Origin rejection ([internal/server/server_test.go](../internal/server/server_test.go), [internal/server/httpsecurity/host_origin_test.go](../internal/server/httpsecurity/host_origin_test.go)), but no end-to-end MCP exchange verifies `MCP-Protocol-Version` handling or the authenticated endpoint. |
| Legacy HTTP+SSE transport | Partial | The separate `/sse` and `/message` implementation remains available ([internal/server/server.go](../internal/server/server.go)). In this specification release, HTTP+SSE has been replaced by Streamable HTTP, so this is a backward-compatibility/custom transport rather than the current standard HTTP transport. |
| Tools: discovery, call, input schemas, text/error results | Supported | AKS-MCP registers Kubernetes and Azure tools via `AddTool` ([internal/server/server.go](../internal/server/server.go)); handlers return MCP text or `isError` tool results ([internal/tools/handler.go](../internal/tools/handler.go)). This covers the core `tools/list` and `tools/call` surface described in the [tools specification](https://modelcontextprotocol.io/specification/2025-06-18/server/tools). |
| Tool list-change notifications | Partial | AKS-MCP registers tools only during initialization. The pinned [mcp-go v0.54.1 server](https://github.com/mark3labs/mcp-go/blob/v0.54.1/server/server.go) automatically emits `notifications/tools/list_changed` when its tool registry changes, but this worktree contains no AKS-MCP runtime mutation path or contract test for that SDK-provided behavior. A static list does not require a notification. |
| Prompt listing and retrieval | Supported | The server advertises prompt capability and, except in token-auth-only mode, registers two prompt handlers: cluster metadata and health ([internal/server/server.go](../internal/server/server.go), [internal/prompts/cluster.go](../internal/prompts/cluster.go), [internal/prompts/health.go](../internal/prompts/health.go)). This implements the `prompts/list`/`prompts/get` model in the [prompts specification](https://modelcontextprotocol.io/specification/2025-06-18/server/prompts). |
| Prompt list-change notifications | Partial | `WithPromptCapabilities(true)` advertises `listChanged`. The pinned [mcp-go v0.54.1 server](https://github.com/mark3labs/mcp-go/blob/v0.54.1/server/server.go) automatically emits the corresponding notification when its prompt registry changes, but AKS-MCP registers prompts at startup only and has no runtime mutation path or contract test for it. |
| Resources: list/read/templates | Partial | The server advertises resources with both `subscribe` and `listChanged` (`WithResourceCapabilities(true, true)`, [internal/server/server.go](../internal/server/server.go)). A repository-wide static search found no `AddResource` or `AddResourceTemplate` call, so there is no AKS resource, resource template, or application read handler to expose through the [resources protocol](https://modelcontextprotocol.io/specification/2025-06-18/server/resources). The SDK may answer protocol requests, but AKS-MCP exposes no useful resource content. |
| Resource subscriptions and change notifications | Partial | The advertised sub-capabilities enable the SDK's subscription/list-change protocol paths, but AKS-MCP has no registered resources and no application-level update path that could invoke them. |
| MCP logging messages | Partial | The server advertises logging through `WithLogging` ([internal/server/server.go](../internal/server/server.go)), while application logging uses the local logger and no MCP logging-message call is present. Thus the capability is SDK-enabled, but structured server-to-client MCP logs are not evidenced. |
| Completion API | Missing | Prompt arguments and resource templates can use the completion API under the specification, but this worktree has no completion handler or completion-capability configuration. |
| Progress and cancellation utilities | Missing | Tool execution accepts a Go `context.Context`, but no MCP progress-token handling, progress notification, or cancellation-notification handling is registered. This is especially relevant to long-running Azure/Kubernetes calls but remains optional under MCP. |
| Structured tool results and output schemas | Missing | Tool wrappers always return `mcp.NewToolResultText` or `mcp.NewToolResultError` ([internal/tools/handler.go](../internal/tools/handler.go)); no structured content or output schemas are registered. Both are optional, but the 2025-06-18 tools specification supports them. |
| Server-initiated client features (roots, sampling, elicitation) | Missing | No calls or handlers for roots, sampling, or elicitation were found. These are optional client capabilities, so their absence is not a base-protocol defect. |

## Observations and risk framing

- Tool and prompt support are the active AKS-MCP product surface. Resources are
  currently capability-advertised but have no AKS-MCP content behind them.
- The `CreateResourceHandler` name is an internal tool-handler abstraction; it
  still returns `mcp.CallToolResult` and is not a MCP `resources/read` handler
  ([internal/tools/handler.go](../internal/tools/handler.go)).

## Single recommended enhancement (do not implement in this run)

**Stop advertising resource subscriptions and resource-list changes until
AKS-MCP registers an actual MCP resource.** Concretely, remove
`server.WithResourceCapabilities(true, true)` from the `NewMCPServer` options
in `internal/server/server.go` (or replace it with a capability configuration
only when a real resource is added).

This is a one-line, backwards-compatible capability-negotiation correction:
clients that already use tools and prompts are unaffected, and new clients will
not negotiate unused `resources/subscribe` or list-change behavior. It is
upstream-friendly because it uses the existing `mcp-go` option surface, needs no
SDK fork or protocol extension, and can be covered by a small initialize-result
contract test in a future change. No implementation is included in this audit.

## Evidence and limitations

Commands used for the source evidence:

```text
rtk git rev-parse --short HEAD                    # 8e84bce
rtk rg -n 'AddResource|AddResourceTemplate|...' --glob '*.go' .
                                                  # no resource registrations
rtk rg -n 'MCPServer|NewStreamableHTTPServer|NewSSEServer|...' internal test
                                                  # server and transport paths
rtk nl -ba internal/server/server.go ...          # cited registrations/routes
```

The specification was read from the version-pinned official pages above. No
server process was started and no cluster, credential, deployment, or Azure
operation was invoked. Go is not installed in this execution environment, so
this audit could not run the repository's Go test suite; the matrix deliberately
does not treat unexecuted SDK behavior as verified wire conformance.
