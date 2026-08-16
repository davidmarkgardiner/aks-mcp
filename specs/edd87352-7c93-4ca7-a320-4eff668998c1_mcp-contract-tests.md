# Issue #8 — MCP protocol and safety contract slice

## Baseline and scope

- Work only in this isolated worktree, based on `origin/main` at `8e84bce` (`github.com/mark3labs/mcp-go v0.54.1`). There is no repository `AGENTS.md` at this baseline.
- Deliver the smallest new wire-level slice: deterministic Streamable HTTP initialization/capability negotiation and post-initialization session/header coverage, plus truthful compatibility and operating documentation. Do not change production behavior, transports, OAuth, tool registration, access levels, Host/Origin policy, dependencies, chart, or deployment assets.
- Pin the contract to literal MCP version `2025-11-25`, the current protocol version supplied by the already-pinned `mcp-go v0.54.1`. Keep the literal in the fixture/test rather than deriving it from `mcp.LATEST_PROTOCOL_VERSION`, so a dependency protocol change fails the contract until it is consciously reviewed and documented.
- PR #17 is evidence only: it was closed without merge because its factory artifact-path gate failed, not because of a recorded code-review/CI result. Do not revive its branch or copy it wholesale. In particular, replace its stale `2025-06-18` baseline with the current dependency-supported version, avoid a test client that merely rejects bad input before exercising the route, and retain the newer `origin/main` Host/Origin tests rather than duplicating them.
- Every added test must stay process-local: static credential-free fixtures, `httptest`, token-auth-only configuration, and the existing injected `fakeProc`. It must not execute tools, invoke `az`/`kubectl`, inspect a kube context, contact Azure, require credentials, or require any AKS/kind cluster. The Geekom kind homelab is not AKS and is out of scope.

## Files and changes

1. **Add a focused protocol contract suite and stable fixtures.**
   - Add `internal/server/testdata/mcp_initialize_2025-11-25.json` with a fixed JSON-RPC 2.0 initialize request: a fixed request ID and synthetic client identity/capabilities, protocol version `2025-11-25`, and no secrets, cluster IDs, timestamps, ports, or generated values.
   - Add `internal/server/testdata/mcp_ping.json` with a fixed JSON-RPC 2.0 ping request and fixed ID.
   - Add `internal/server/mcp_contract_test.go` in package `server`. Reuse `createTestConfig` and the existing fake az-cli process from `server_test.go`; set `Transport` to `streamable-http`, bind host to loopback, and enable `TokenAuthOnly` before `Service.Initialize`. This skips Azure tool registration/login and never dispatches a tool.
   - Assemble the tested route exactly as production does: create the custom HTTP server, create `mcp-go`'s Streamable HTTP server with the same `X-Azure-Token` context hook, then mount it through `installStreamableMCPHandler`. Serve that mux using `httptest.NewServer`; do not invent a replacement MCP handler or open a fixed port.
   - Place a concurrency-safe recording handler immediately inside the mounted route (therefore after the existing Host/Origin middleware) solely to observe requests that actually reached the MCP transport. Use the `httptest` server's client, not `http.DefaultClient`.
   - Post the initialize fixture to `/mcp` through the permitted loopback route. Decode the real response once and assert: HTTP 200; media type `application/json` (allowing parameters); JSON-RPC `2.0`; preserved fixture request ID; nonempty `Mcp-Session-Id`; server name `AKS MCP`; server version equal to `version.GetVersion()`; and negotiated protocol exactly the literal pinned version.
   - Decode capabilities field-by-field and assert the currently configured server contract: `resources.subscribe` and `resources.listChanged` are true, `prompts.listChanged` is true, `logging` is present, and the registered-tool capability has the expected `listChanged` value. Assert that sampling, roots, elicitation, completions, and tasks are absent. These literal expectations must fail if an advertised capability is removed, altered, or added without an explicit contract/matrix update; do not treat capability advertisement as proof of useful resources, prompts, or tool schemas.
   - Reuse the returned session ID for the ping fixture and send `Content-Type`, `Accept`, `Mcp-Session-Id`, and `Mcp-Protocol-Version: 2025-11-25`. Assert the JSON-RPC success envelope/ID and use the recording route boundary to prove the exact negotiated protocol header reached the actual Streamable HTTP handler. Include focused negative assertions around the request-builder/header setup (missing or wrong protocol header cannot satisfy the contract helper) and an observation assertion so a missing, misspelled, or wrong-valued header fails the suite. Do not claim that `mcp-go` currently rejects arbitrary protocol headers server-side when it does not; document that server-side enforcement as deferred rather than manufacturing it in the test.
   - Do not duplicate `internal/server/httpsecurity/host_origin_test.go` or the existing `TestStreamableHTTPHostOriginMiddleware*` coverage in `server_test.go`. The new test uses their normal loopback-admitted route, while the compatibility matrix links those existing tests as the separate safety evidence.

2. **Publish an honest compatibility matrix and focused invocation guidance.**
   - Add `docs/mcp-compatibility.md`. Identify the source baseline, exact `mcp-go` version, literal protocol pin, and state prominently that it is a local contract slice, not MCP certification.
   - Use the statuses **implemented and contract-tested**, **implemented but not covered by this slice**, **optional/not implemented**, and **deferred**. Distinguish: initialization/version negotiation; Streamable HTTP session ID and client request protocol-header propagation; the advertised resources/prompts/logging/tools capability shape; and the existing Host/Origin boundary.
   - Explicitly say resource/prompt capability advertisement does not mean a useful resource or prompt is registered in this token-auth-only test setup, and that tool schemas/invocation are not tested. Mark sampling, roots, elicitation, completions, and Tasks optional/not implemented; mark tool schemas/invocation, progress, cancellation, OAuth end-to-end, server-side protocol-header enforcement, and live Azure/AKS acceptance deferred. Link the new suite and existing Host/Origin test locations.
   - State that the suite uses fixtures/fakes and an in-process HTTP server only, needs neither Azure credentials nor an AKS cluster, and does not classify or use the Geekom kind homelab as AKS.
   - In the README testing/development section, add a compact “MCP contract testing” subsection that links the matrix and provides the focused command:
     ```bash
     go test -tags withoutebpf ./internal/server -run '^TestMCP.*Contract$' -count=1
     ```
     Also list the full local Go gates below and repeat the no-credentials/no-cluster boundary without adding deployment instructions.

## Verification and delivery

1. Format only changed Go files with `gofmt -w`, then run `git diff --check` and record command exit statuses.
2. Run the focused suite exactly as documented:
   ```bash
   go test -tags withoutebpf ./internal/server -run '^TestMCP.*Contract$' -count=1
   ```
3. Run the repository-level deterministic gates and record their exit statuses and material output:
   ```bash
   go vet -tags withoutebpf ./...
   go test -tags withoutebpf ./...
   ```
   If any gate fails, preserve the evidence, identify whether it is a change regression or pre-existing/environmental blocker, and do not substitute a live-cluster check or conceal the failure.
4. Obtain and record an independent review of the final diff, concentrating on the literal version pin, real-route assembly, response/capability assertions, session/header observation, fixture determinism, truthful matrix wording, and preservation of existing safety policy. Address in-scope findings and rerun affected checks.
5. Confirm the final diff contains only the test, fixtures, documentation/README updates, and required planning artifact. Commit with a focused conventional subject, push only this isolated `factory/aks-mcp-personal-fork/edd87352-7c93-4ca7-a320-4eff668998c1` branch to `origin`, and open exactly one **draft** PR in `davidmarkgardiner/aks-mcp` against that fork’s `main`, referencing Issue #8 and the recorded validation/review evidence.
6. Stop once the draft PR exists. Do not merge, deploy, mutate any cluster, run Docker/kind/Helm/kubectl/az/login, handle credentials, use DeepSeek/OpenRouter/API-key routes, push to `upstream`, or open an upstream PR.

## Acceptance evidence

- The focused test sends the fixed `2025-11-25` initialize and ping fixtures through AKS-MCP’s production-equivalent Streamable HTTP assembly and validates the JSON-RPC envelope, server identity, exact negotiated version, actual capability shape, session ID, and observed negotiated request header.
- The test fails on an advertised-capability mismatch or a missing/misvalued client protocol header, without overstating unsupported server-side header rejection.
- The compatibility matrix and README accurately state what is contract-tested, what existing Host/Origin tests cover, and what remains deferred; neither claims Azure/AKS validation nor treats kind as AKS.
- Recorded local focused, vet, full-test, formatting/diff, and independent-review evidence accompanies the single scoped commit and draft fork PR.
