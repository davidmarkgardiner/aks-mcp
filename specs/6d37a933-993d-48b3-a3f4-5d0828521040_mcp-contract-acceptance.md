# Issue #8 contract slice — Codex delivery acceptance

## Baseline, scope, and constraints

- Work only in the isolated branch `factory/aks-mcp-personal-fork/6d37a933-993d-48b3-a3f4-5d0828521040`, based on `origin/main` at `8e84bce`. This supersedes the prior plan's `b19e2a93-0d79-4651-a285-b85726af6eab` branch identity.
- There is no repository `AGENTS.md` at this baseline. Preserve the current production route, Host/Origin policy, OAuth behavior, access-level behavior, and read-only defaults; this slice should not need production Go changes.
- Deliver only wire-level Streamable HTTP MCP initialization/capability-negotiation coverage plus compatibility/operating documentation. Exclude tool schemas or invocation, progress, cancellation, Tasks, resource implementation, OAuth end-to-end coverage, live Azure/AKS acceptance, deployment, and cluster checks.
- Pin the contract expectation to MCP protocol `2025-06-18` in a local test constant and fixture. Do not use `mcp.LATEST_PROTOCOL_VERSION`: the pinned `github.com/mark3labs/mcp-go v0.54.1` currently advertises a newer protocol version.
- All test work must be process-local: `httptest`, static credential-free JSON fixtures, inert config, and the existing `fakeProc` injection. Do not call `az`, `kubectl`, `kind`, Docker, a `red`/Geekom context, Azure, or any credential operation. The Geekom kind homelab is not AKS and is not a prerequisite.

## Implementation

1. **Add deterministic Streamable HTTP contract fixtures and tests.**
   - Create `internal/server/testdata/` fixtures for the JSON-RPC `initialize` request (protocol `2025-06-18`, fixed synthetic client identity/capabilities and request ID) and the harmless post-initialization `ping` request. Keep fixtures free of credentials, timestamps, ports, cluster identifiers, and generated session IDs.
   - Add `internal/server/mcp_contract_test.go` in package `server`. Build a real `Service` with `createTestConfig`, set its transport and bind host for the Streamable HTTP/loopback route, use `WithAzCliProcFactory` with the existing `fakeProc`, and configure token-auth-only mode so initialization registers no Azure-backed components or real Azure CLI login. Initialize the service without invoking tools.
   - Reproduce the production Streamable HTTP assembly rather than introducing a replacement handler: obtain `createCustomHTTPServerWithHelp404`, create `server.NewStreamableHTTPServer` with the production-equivalent context hook, mount it through `installStreamableMCPHandler`, and serve its handler via `httptest.NewServer`.
   - Post the static initialize fixture to `/mcp` using an allowed loopback Host. Decode the real JSON-RPC response and assert HTTP 200, JSON content type, JSON-RPC `2.0`, preserved request ID, a nonempty `Mcp-Session-Id`, server name `AKS MCP`, server version equal to `version.GetVersion()`, and negotiated protocol version exactly `2025-06-18`.
   - Assert advertised capabilities field by field: resources contains `subscribe` and `listChanged`, prompts contains `listChanged`, and logging is present. Assert unconfigured/unsupported capabilities (sampling, roots, elicitation, completions, and Tasks) are absent. This must test the actual decoded capability shape rather than merely confirming decoding succeeds.
   - Reuse the returned session ID to send the fixture ping through a small local contract-client helper. Require that helper to send `Mcp-Protocol-Version: 2025-06-18`; use a recording transport/handler boundary to prove that exact header reaches the request. Add table-driven missing and wrong-header cases that the helper rejects before they can be accepted as valid negotiated exchanges. Do not claim the server implements unsupported features merely to make these cases pass.
   - Retain existing Host/Origin security tests and add no allowlist exceptions. The contract test should only establish that its allowed loopback request reaches the normal route.

2. **Document compatibility and local operation truthfully.**
   - Add `docs/mcp-compatibility.md` identifying `origin/main`/the source baseline, contract baseline `2025-06-18`, and the pinned `mcp-go v0.54.1` dependency.
   - Include a compatibility matrix using only these statuses: **implemented and contract-tested**, **implemented but not covered by this slice**, **optional/not implemented**, and **deferred**. Cover initialization/version negotiation, Streamable HTTP session/protocol-header exchange, and the existing Host/Origin boundary with links to the new contract suite and existing security tests.
   - Distinguish advertised capabilities from operational behavior: document the precise resources/prompts/logging response claims, state that no resource is made useful by this work without a registered resource, and defer tools/schemas, progress, cancellation, Tasks, OAuth end-to-end, and live Azure/AKS claims. State explicitly that this is not MCP certification.
   - Update the README Development/testing section to link the matrix and show the focused tagged local command `go test -tags withoutebpf ./internal/server -run '^TestMCP.*Contract$' -count=1`, plus the full admitted tagged gates below. Explain that the suite uses local fixtures/fakes, needs neither Azure credentials nor AKS, and does not treat the Geekom kind homelab as AKS.

3. **Verify with the admitted local gates and record actual outcomes.**
   - Run `gofmt -w` on changed Go files and `git diff --check`.
   - Run the focused suite: `go test -tags withoutebpf ./internal/server -run '^TestMCP.*Contract$' -count=1`.
   - Run the required repository gates exactly with the repository's `withoutebpf` build tag: `go vet -tags withoutebpf ./...` and `go test -tags withoutebpf ./...`. Record each command, exit status, and material output in delivery evidence; do not replace these with untagged commands or infer success from output text.
   - Do not run live Azure, AKS, kind/Geekom, Docker, Azure login, Helm, deployment, mutation, credential, upstream-push, merge, DeepSeek, or OpenRouter actions. If a local gate fails, preserve the exit-status evidence and distinguish an unchanged environmental/dependency blocker from a change regression rather than masking it.

4. **Complete the constrained fork delivery.**
   - Obtain an independent Claude Sonnet 5 review of the final diff, focused on the fixed protocol version, JSON-RPC/session/header checks, fixture determinism, capability truthfulness, and preservation of Host/Origin/read-only safety. Save the verdict/findings in delivery evidence, address all in-scope findings, and rerun affected verification.
   - Confirm final status/diff contains only the new test/fixtures, compatibility docs, README update, and any required planning artifact. Commit the implementation and documentation with a focused conventional commit subject.
   - Push only `factory/aks-mcp-personal-fork/6d37a933-993d-48b3-a3f4-5d0828521040` to David's `origin` fork. Create exactly one draft PR in `davidmarkgardiner/aks-mcp` targeting that fork's `main`, reference Issue #8, and report the local gate/review evidence. Do not push to `upstream`, open an upstream PR, merge, deploy, or change credentials.

## Acceptance evidence

- The new suite exercises AKS-MCP's real Streamable HTTP route with static `2025-06-18` initialization/ping fixtures, validates identity, negotiated version, capability shape, session ID, and protocol header propagation/rejection cases.
- The compatibility matrix and README define the tested boundary and reproduce the no-cluster tagged command without overstating unsupported MCP or Azure/AKS behavior.
- `go vet -tags withoutebpf ./...` and `go test -tags withoutebpf ./...` have recorded exit-status evidence, along with focused-test and diff/format results.
- The final isolated fork branch contains the scoped committed documentation and test slice, an independent Claude Sonnet 5 review record, and one draft fork PR, with no prohibited infrastructure or upstream side effects.
