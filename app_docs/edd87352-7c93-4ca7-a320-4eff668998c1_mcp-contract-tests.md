# MCP protocol & safety contract tests (Issue #8)

**Change:** adds version-pinned, deterministic MCP protocol contract coverage plus an honest compatibility matrix to the AKS-MCP personal fork (`davidmarkgardiner/aks-mcp`), delivered from `origin/main` at `8e84bce`. Everything is process-local: **no Azure credentials, no AKS cluster, no `az`/`kubectl` invocation, no tool dispatch, no live Azure contact.** The Geekom kind homelab is explicitly *not* AKS and is not used. The closed PR #17 was inspected as evidence only — this slice re-pins away from its stale `2025-06-18` baseline and does not revive its branch.

## Why it matters

The contract is pinned to the literal MCP protocol version **`2025-11-25`** — the current version supplied by the already-pinned `mcp-go v0.54.1`. The literal is deliberately **not** derived from `mcp.LATEST_PROTOCOL_VERSION`, so if the dependency ever bumps its supported protocol version, the suite fails until the change is consciously reviewed and the compatibility matrix updated. This is the "tripwire" property of the whole slice.

## Files that carry it

- **`internal/server/mcp_contract_test.go`** (new, 575 lines) — the contract suite, package `server`. Three tests:
  - `TestMCPInitializeNegotiationContract` — posts the pinned initialize fixture through the production-equivalent route assembly and asserts: HTTP 200 with `application/json`; JSON-RPC 2.0 envelope preserving the fixture's request ID; a non-empty `Mcp-Session-Id`; `serverInfo.name` = `AKS MCP` and `serverInfo.version` = `version.GetVersion()`; negotiated `protocolVersion` = exactly the pinned literal; and the **exact advertised capability shape** — key set `{logging, prompts, resources, tools}` with `resources.subscribe`/`resources.listChanged` true, `prompts.listChanged` true, `tools.listChanged` true, `logging` present, and sampling/roots/elicitation/completions/tasks/experimental/extensions absent. Any added, removed, or altered advertisement fails the test.
  - `TestMCPPingSessionHeaderContract` — initializes a session, then sends the ping fixture reusing the session ID and `Mcp-Protocol-Version: 2025-11-25`. Asserts the JSON-RPC success envelope with an empty `{}` result, and — via an `mcpRequestRecorder` mounted immediately inside the `/mcp` route (i.e. after the existing Host/Origin middleware) — proves the exact negotiated header value actually reached the Streamable HTTP handler.
  - `TestMCPPinnedProtocolHeaderBuilderContract` — pins the client-side request builder: it refuses a missing, stale (`2025-06-18`, the PR #17 baseline), or unknown protocol version and a missing session ID; a positive control verifies the full post-initialization header set.
  - **Route assembly is production-equivalent:** `createTestConfig("readonly", nil)` with `Transport=streamable-http`, `Host=127.0.0.1`, `TokenAuthOnly=true`; the injected `fakeProc` az-cli factory via `WithAzCliProcFactory`; `createCustomHTTPServerWithHelp404`; `server.NewStreamableHTTPServer` with the same `X-Azure-Token` context hook; mounted through `installStreamableMCPHandler` (so Host/Origin middleware runs); served by `httptest.NewServer` (no fixed port). Token-auth-only mode skips Azure CLI auth and Azure component/prompt registration.
- **`internal/server/testdata/mcp_initialize_2025-11-25.json`** and **`internal/server/testdata/mcp_ping.json`** (new) — static, credential-free JSON-RPC fixtures with fixed request IDs (9001 / 9002); no secrets, cluster IDs, timestamps, or generated values.
- **`docs/mcp-compatibility.md`** (new, 92 lines) — the compatibility matrix. Records the baseline (`8e84bce`, mcp-go v0.54.1, where the pin lives), the test-environment boundary, four statuses (implemented and contract-tested / implemented but not covered by this slice / optional-not-implemented / deferred), a per-feature matrix, and two honesty caveats: (1) **capability advertisement is not proof of a useful registered tool/resource/prompt** — in token-auth-only setup only the kubectl tool set registers and nothing is dispatched; (2) mcp-go v0.54.1 does **not** enforce `Mcp-Protocol-Version` server-side, so that enforcement is documented as *deferred*, not manufactured in tests. Host/Origin (DNS-rebinding) safety is covered by the pre-existing `internal/server/httpsecurity/host_origin_test.go` and `TestStreamableHTTPHostOriginMiddleware*` in `internal/server/server_test.go` — linked, not duplicated. Live Azure/AKS acceptance is marked deferred/unverified.
- **`README.md`** (+16 lines) — new "MCP Contract Testing" subsection in the testing area, linking the matrix and giving the focused run command plus the full local Go gates.
- **`specs/edd87352-7c93-4ca7-a320-4eff668998c1_mcp-contract-tests.md`** (new) — the planning spec that defined this slice (scope, fixtures, assertions, verification, and the stop-at-draft-PR delivery boundary).

## How to verify

From the repository root:

```bash
# Focused MCP contract suite only
go test -tags withoutebpf ./internal/server -run '^TestMCP.*Contract$' -count=1

# Full local Go gates
go vet -tags withoutebpf ./...
go test -tags withoutebpf ./...
```

All three commands are credential-free and cluster-free by design. Any claim requiring live Azure/AKS remains unverified and is recorded as such in the matrix — do not substitute a cluster check for these gates.
