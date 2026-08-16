# MCP compatibility matrix

> **This is a local contract-test slice, not MCP certification.** It records
> what the tests in this repository actually verify about AKS-MCP's MCP
> protocol behavior at a pinned point in time. It is not an official
> compatibility claim, and it does not represent validation against any MCP
> conformance suite.

## Baseline

| Item | Value |
| --- | --- |
| Source baseline | `origin/main` at `8e84bce` of this fork (`davidmarkgardiner/aks-mcp`) |
| MCP server library | [`github.com/mark3labs/mcp-go` v0.54.1](https://github.com/mark3labs/mcp-go) (pinned in `go.mod`) |
| Pinned protocol version | `2025-11-25` — the literal current protocol version supplied by mcp-go v0.54.1 |
| Where the pin lives | `internal/server/testdata/mcp_initialize_2025-11-25.json` and `internal/server/mcp_contract_test.go` (deliberately **not** derived from `mcp.LATEST_PROTOCOL_VERSION`, so a dependency protocol bump fails the contract until it is consciously reviewed and this page updated) |
| Contract suite | [`internal/server/mcp_contract_test.go`](../internal/server/mcp_contract_test.go) |
| Existing safety evidence | [`internal/server/httpsecurity/host_origin_test.go`](../internal/server/httpsecurity/host_origin_test.go) and the `TestStreamableHTTPHostOriginMiddleware*` tests in [`internal/server/server_test.go`](../internal/server/server_test.go) |

Run the contract suite with:

```bash
go test -tags withoutebpf ./internal/server -run '^TestMCP.*Contract$' -count=1
```

## Test-environment boundary

The contract suite is fully process-local and deterministic:

- Static, credential-free fixtures under `internal/server/testdata/` and an
  in-process `httptest` server (no fixed port, no external listener).
- Service initialization uses the repository's test config helper, the
  injected fake az-cli process, and **token-auth-only** mode, which skips
  Azure CLI authentication and Azure component/prompt registration.
- No tool is dispatched, no `az` or `kubectl` binary is invoked, no Azure
  endpoint is contacted, and **no Azure credentials are required**.
- **No AKS cluster is required or used.** In particular, a local kind
  homelab (such as the Geekom kind cluster, kube context `red` /
  `kind-homelab`) is *not* AKS and is never classified or used as AKS by
  these tests.

## Statuses

- **Implemented and contract-tested** — exercised end-to-end over HTTP by the pinned contract suite.
- **Implemented but not covered by this slice** — present in the code path, not asserted by these tests.
- **Optional / not implemented** — the server does not advertise or implement the feature.
- **Deferred** — deliberately out of scope for this slice; see notes.

## Matrix

| MCP feature / behavior | Status | Notes |
| --- | --- | --- |
| `initialize` request handling and version negotiation | Implemented and contract-tested | `TestMCPInitializeNegotiationContract` posts the pinned `2025-11-25` fixture through the production-equivalent route assembly and asserts the negotiated `protocolVersion` echoes exactly the pinned literal. |
| JSON-RPC 2.0 response envelope (request-ID preservation, success/error shape) | Implemented and contract-tested | Asserted for both `initialize` and `ping` responses. |
| Streamable HTTP session ID issuance (`Mcp-Session-Id` on initialize response) | Implemented and contract-tested | Asserted non-empty; the ping request reuses it. |
| Post-initialization client protocol-header propagation (`Mcp-Protocol-Version: 2025-11-25` on subsequent requests) | Implemented and contract-tested | `TestMCPPingSessionHeaderContract` proves the exact negotiated header value reached the mounted Streamable HTTP handler via a recording boundary inside the route, and the request builder refuses missing/stale/unknown versions. |
| Advertised capabilities shape (`resources.subscribe`, `resources.listChanged`, `prompts.listChanged`, `logging`, `tools.listChanged`) | Implemented and contract-tested | The suite pins the exact capability key set and each field's value, so any added, removed, or altered advertisement fails the contract. |
| Host/Origin HTTP boundary (DNS-rebinding protection) | Implemented and contract-tested | Covered **separately** by `internal/server/httpsecurity/host_origin_test.go` and `TestStreamableHTTPHostOriginMiddleware*` in `internal/server/server_test.go` — that is the authoritative safety evidence, linked here rather than duplicated. The MCP contract suite deliberately uses only the normal loopback-admitted route. |
| Server-side rejection of unknown `Mcp-Protocol-Version` header values | Deferred | mcp-go v0.54.1 does **not** enforce this header server-side on the Streamable HTTP transport, and the suite does not manufacture such a claim. The tests constrain the contract client and observe the wire; enforcement remains deferred to a dependency update or explicit server middleware. |
| Tool registration schemas and tool invocation | Deferred | No tool is dispatched by this suite. Capability advertisement is *not* proof of a useful registered tool set; see the caveat below. |
| Resources/prompts listing, reading, and subscription behavior | Deferred | See the capability caveat below. |
| Progress notifications and request cancellation | Deferred | Not exercised. |
| SSE upgrade / GET listening stream / DELETE session termination | Implemented but not covered by this slice | Provided by the mcp-go transport; not asserted here. |
| OAuth 2.1 end-to-end (dynamic client registration, token flow, bearer enforcement) | Deferred | OAuth middleware exists in production code; this suite runs OAuth-disabled, token-auth-only, and makes no claims about it. End-to-end OAuth coverage is deferred. |
| Sampling | Optional / not implemented | Not advertised by this server. |
| Roots | Optional / not implemented | Not advertised by this server. |
| Elicitation | Optional / not implemented | Not advertised by this server. |
| Completions | Optional / not implemented | Not advertised by this server. |
| Tasks | Optional / not implemented | Not advertised by this server. |
| Live Azure / AKS acceptance | Deferred | This repository's tests never contact Azure or an AKS cluster; any claim requiring live AKS must be marked unverified and preserved separately. |

## Capability-advertisement caveat

The initialize response advertises `resources`, `prompts`, `logging`, and
`tools` capabilities because the server is constructed with
`WithResourceCapabilities(true, true)`, `WithPromptCapabilities(true)`,
`WithLogging()`, and registers tools during initialization. **Capability
advertisement is not proof of a useful registered resource, prompt, or tool.**
In the token-auth-only test setup used by the contract suite, no prompts and
no Azure components are registered (only the kubectl tool set), and neither
resource/prompt listing nor tool schemas/invocation are exercised. Treat the
capability-shape assertions as a wire-contract pin, not a feature
certification.

## Relationship to closed PR #17

The earlier attempt (PR #17, closed unmerged) pinned its contract to the
stale `2025-06-18` protocol version and was closed for non-code reasons; it
is used here only as evidence. This slice re-pins to `2025-11-25` (the
current version supported by the already-pinned mcp-go v0.54.1), avoids
duplicating the newer Host/Origin coverage that exists on `origin/main`, and
does not revive that branch.
