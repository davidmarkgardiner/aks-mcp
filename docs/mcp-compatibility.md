# MCP compatibility matrix

This matrix describes the local Streamable HTTP contract slice based on
`origin/main` at `8e84bce` and `github.com/mark3labs/mcp-go v0.54.1`. The
contract expectation is deliberately pinned to MCP protocol `2025-06-18`; it
does not follow the dependency's latest advertised protocol version. This is
not MCP certification.

| Surface | Status | Evidence / boundary |
| --- | --- | --- |
| Initialization and version negotiation (`2025-06-18`) | **implemented and contract-tested** | [`TestMCPStreamableHTTPContract`](../internal/server/mcp_contract_test.go) posts a deterministic initialize fixture and verifies JSON-RPC identity, server identity, and negotiated version. |
| Streamable HTTP session and `Mcp-Protocol-Version` exchange | **implemented and contract-tested** | The same suite receives an `Mcp-Session-Id`, reuses it for `ping`, verifies the pinned protocol header at the handler boundary, and rejects missing/wrong negotiated versions before sending. |
| Host/Origin boundary | **implemented but not covered by this slice** | Existing [`TestStreamableHTTPHostOriginMiddleware`](../internal/server/server_test.go) and related host/origin tests retain the security boundary. The contract suite uses only its normal loopback route. |
| Advertised resources (`subscribe`, `listChanged`), prompts (`listChanged`), and logging | **implemented and contract-tested** | The initialize response is decoded field-by-field. This work does not register a useful resource, so advertising resource capability alone does not make a resource operational. |
| Sampling, roots, elicitation, completions, and Tasks | **optional/not implemented** | These capabilities are asserted absent from this server's contract response. |
| Tools and schemas, progress, cancellation, OAuth end-to-end | **deferred** | Outside this initialization/capability-negotiation slice. |
| Live Azure and AKS acceptance | **deferred** | No Azure credentials or AKS cluster are required or exercised. The Geekom kind homelab is not AKS. |

The suite is process-local: static credential-free JSON fixtures, `httptest`,
inert configuration, and the existing fake Azure CLI process. It does not run
tools, contact Azure, or depend on a Kubernetes cluster.
