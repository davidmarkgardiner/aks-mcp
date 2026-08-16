package server

// MCP protocol contract tests (issue #8).
//
// This suite pins the wire-level Streamable HTTP contract that AKS-MCP
// exposes through mcp-go v0.54.1 to the literal MCP protocol version
// "2025-11-25". The literal is deliberately not derived from
// mcp.LATEST_PROTOCOL_VERSION: if the pinned dependency ever changes the
// protocol version it supports, these tests fail until the change is
// consciously reviewed and docs/mcp-compatibility.md is updated.
//
// Everything here is process-local and deterministic: static fixtures from
// testdata, an httptest server (no fixed port), the existing createTestConfig
// helper, the injected fake az-cli process, and token-auth-only mode so that
// Initialize skips Azure CLI authentication and Azure tool/prompt
// registration. No tool is ever dispatched, no az/kubectl binary is invoked,
// no Azure endpoint is contacted, and no cluster (AKS or otherwise) is
// required. The Host/Origin security policy itself is covered separately by
// internal/server/httpsecurity/host_origin_test.go and the
// TestStreamableHTTPHostOriginMiddleware* tests in server_test.go; this suite
// deliberately uses only the normal loopback-admitted route.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/Azure/aks-mcp/internal/azcli"
	"github.com/Azure/aks-mcp/internal/ctx"
	"github.com/Azure/aks-mcp/internal/version"
	"github.com/mark3labs/mcp-go/server"
)

// mcpPinnedProtocolVersion is the literal MCP protocol version this contract
// is pinned to. It must match the version negotiated with the mcp-go
// dependency. Do not replace it with mcp.LATEST_PROTOCOL_VERSION: the whole
// point of the pin is that a dependency protocol bump breaks this suite
// until the change is reviewed and documented in docs/mcp-compatibility.md.
const mcpPinnedProtocolVersion = "2025-11-25"

// recordedMCPRequest captures the wire-level facts observed at the mounted
// /mcp route boundary: that a request actually reached the Streamable HTTP
// transport, and which MCP transport headers it carried.
type recordedMCPRequest struct {
	Method          string
	Path            string
	SessionID       string
	ProtocolVersion string
}

// mcpRequestRecorder is a concurrency-safe observer mounted immediately
// inside the /mcp route — after the existing Host/Origin security
// middleware — that records every request which reaches the mounted MCP
// transport and then delegates to it. It exists solely for observation; it
// never alters requests or responses.
type mcpRequestRecorder struct {
	mu       sync.Mutex
	requests []recordedMCPRequest
}

func (r *mcpRequestRecorder) record(req *http.Request) {
	rec := recordedMCPRequest{
		Method:          req.Method,
		Path:            req.URL.Path,
		SessionID:       req.Header.Get("Mcp-Session-Id"),
		ProtocolVersion: req.Header.Get("Mcp-Protocol-Version"),
	}
	r.mu.Lock()
	r.requests = append(r.requests, rec)
	r.mu.Unlock()
}

// wrap returns a handler that records req and then delegates to next.
func (r *mcpRequestRecorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.record(req)
		next.ServeHTTP(w, req)
	})
}

func (r *mcpRequestRecorder) snapshot() []recordedMCPRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedMCPRequest, len(r.requests))
	copy(out, r.requests)
	return out
}

// mcpContractServer holds the production-equivalent route assembly under
// test: the same custom HTTP server, the same Streamable HTTP server with
// the same X-Azure-Token context hook, and the same mount path that
// Service.Run uses — served by an in-process httptest server instead of a
// bound listener.
type mcpContractServer struct {
	ts       *httptest.Server
	recorder *mcpRequestRecorder
}

// startMCPContractServer assembles the Streamable HTTP MCP route exactly the
// way Service.Run does for the streamable-http transport, minus binding a
// TCP listener: the custom HTTP server mux is created, the mcp-go
// Streamable HTTP server is built with the same X-Azure-Token context hook,
// and the handler is mounted at /mcp through installStreamableMCPHandler
// (which applies the existing Host/Origin security middleware). The mux is
// served by httptest.NewServer so no fixed port is opened.
func startMCPContractServer(t *testing.T) *mcpContractServer {
	t.Helper()

	// Dummy Azure environment variables, mirroring the other tests in this
	// package. Nothing here contacts Azure: service initialization runs in
	// token-auth-only mode with the injected fake az-cli process, which
	// skips Azure CLI authentication and Azure component/prompt
	// registration entirely.
	t.Setenv("AZURE_TENANT_ID", "dummy-tenant-id")
	t.Setenv("AZURE_CLIENT_ID", "dummy-client-id")
	t.Setenv("AZURE_CLIENT_SECRET", "dummy-client-secret")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "dummy-subscription-id")

	cfg := createTestConfig("readonly", nil)
	cfg.Transport = "streamable-http"
	cfg.Host = "127.0.0.1"
	cfg.TokenAuthOnly = true

	service := NewService(cfg, WithAzCliProcFactory(func(timeout int) azcli.Proc { return &fakeProc{} }))
	if err := service.Initialize(); err != nil {
		t.Fatalf("service Initialize failed: %v", err)
	}

	customServer := service.createCustomHTTPServerWithHelp404("127.0.0.1:0")
	mux, ok := customServer.Handler.(*http.ServeMux)
	if !ok {
		t.Fatalf("expected *http.ServeMux handler, got %T", customServer.Handler)
	}

	// Same Streamable HTTP server construction as Service.Run, including
	// the X-Azure-Token → context hook.
	streamableServer := server.NewStreamableHTTPServer(
		service.mcpServer,
		server.WithStreamableHTTPServer(customServer),
		server.WithHTTPContextFunc(func(c context.Context, r *http.Request) context.Context {
			if token := r.Header.Get("X-Azure-Token"); token != "" {
				c = context.WithValue(c, ctx.AzureTokenKey, token)
			}
			return c
		}),
	)

	// Mount through the production path so the Host/Origin middleware runs;
	// the recorder sits immediately inside the mounted route and observes
	// exactly the requests that reached the MCP transport.
	recorder := &mcpRequestRecorder{}
	service.installStreamableMCPHandler(mux, recorder.wrap(streamableServer))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &mcpContractServer{ts: ts, recorder: recorder}
}

// do executes req against the mounted route using the httptest server's
// client and returns the response with its fully read body.
func (s *mcpContractServer) do(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	resp, err := s.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("request to mounted /mcp route failed: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return resp, body
}

// initialize performs the pinned initialize handshake over the mounted route
// using the fixture and returns the negotiated session ID.
func (s *mcpContractServer) initialize(t *testing.T) string {
	t.Helper()
	fixture := loadContractFixture(t, "mcp_initialize_2025-11-25.json")
	req, err := mcpInitializePostRequest(s.ts.URL+"/mcp", fixture)
	if err != nil {
		t.Fatalf("failed to build initialize request: %v", err)
	}
	resp, _ := s.do(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize response did not return a non-empty Mcp-Session-Id header")
	}
	return sessionID
}

// loadContractFixture reads a static, credential-free JSON-RPC fixture from
// testdata.
func loadContractFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return raw
}

// mcpInitializePostRequest builds the HTTP POST for an initialize message.
// Per the MCP 2025-11-25 Streamable HTTP transport, initialize carries no
// session ID or protocol-version header yet — those exist only after
// initialization.
func mcpInitializePostRequest(target string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	return req, nil
}

// mcpSessionPostRequest builds a post-initialization JSON-RPC request. MCP
// 2025-11-25 requires the client to send the negotiated protocol version in
// the Mcp-Protocol-Version header on every request after initialization and
// to carry the session ID returned by initialize. The helper refuses to
// build a request whose protocol version is not the pinned contract
// version, so a missing or wrong-valued header cannot silently satisfy this
// suite. Note that this constrains the contract client only: mcp-go v0.54.1
// does not itself reject arbitrary Mcp-Protocol-Version values server-side,
// and this suite does not claim it does (see docs/mcp-compatibility.md).
func mcpSessionPostRequest(target string, body []byte, sessionID, protocolVersion string) (*http.Request, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("post-initialization request requires the Mcp-Session-Id returned by initialize")
	}
	if protocolVersion != mcpPinnedProtocolVersion {
		return nil, fmt.Errorf("post-initialization request requires Mcp-Protocol-Version %q (the version negotiated during initialize), got %q", mcpPinnedProtocolVersion, protocolVersion)
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Mcp-Protocol-Version", protocolVersion)
	return req, nil
}

// mcpContractJSONRPC is the JSON-RPC 2.0 envelope asserted by this suite.
// Result and Error stay raw so each test decodes only what its contract
// covers.
type mcpContractJSONRPC struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// assertJSONRPCSuccessEnvelope checks the shared JSON-RPC success envelope
// contract: version "2.0", the fixture's request ID preserved, a result, and
// no error object.
func assertJSONRPCSuccessEnvelope(t *testing.T, envelope *mcpContractJSONRPC, fixture []byte) {
	t.Helper()
	if envelope.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want %q", envelope.JSONRPC, "2.0")
	}
	if envelope.Error != nil {
		t.Errorf("unexpected JSON-RPC error object: code=%d message=%q", envelope.Error.Code, envelope.Error.Message)
	}
	if len(bytes.TrimSpace(envelope.Result)) == 0 {
		t.Error("expected a non-empty result object in the success response")
	}

	var fixtureEnvelope struct {
		ID any `json:"id"`
	}
	if err := json.Unmarshal(fixture, &fixtureEnvelope); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	var gotID any
	if err := json.Unmarshal(envelope.ID, &gotID); err != nil {
		t.Fatalf("response id %s is not valid JSON: %v", envelope.ID, err)
	}
	if !reflect.DeepEqual(gotID, fixtureEnvelope.ID) {
		t.Errorf("response id = %v, want the fixture request id %v", gotID, fixtureEnvelope.ID)
	}
}

// assertJSONMediaType checks that the response media type is application/json
// (parameters such as charset are allowed).
func assertJSONMediaType(t *testing.T, resp *http.Response) {
	t.Helper()
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("failed to parse response Content-Type %q: %v", resp.Header.Get("Content-Type"), err)
	}
	if mediaType != "application/json" {
		t.Fatalf("response media type = %q, want application/json", mediaType)
	}
}

// TestMCPInitializeNegotiationContract posts the pinned initialize fixture
// through the production-equivalent Streamable HTTP assembly and asserts the
// initialize response contract: HTTP 200 JSON, JSON-RPC 2.0 envelope with the
// fixture's request ID, a non-empty session ID, the AKS MCP server identity
// at the current build version, the exact pinned protocol version, and the
// currently advertised capability shape.
func TestMCPInitializeNegotiationContract(t *testing.T) {
	s := startMCPContractServer(t)

	fixture := loadContractFixture(t, "mcp_initialize_2025-11-25.json")

	var initializeRequest struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"params"`
	}
	if err := json.Unmarshal(fixture, &initializeRequest); err != nil {
		t.Fatalf("initialize fixture is not valid JSON: %v", err)
	}
	if initializeRequest.JSONRPC != "2.0" || initializeRequest.Method != "initialize" {
		t.Fatalf("fixture is not a JSON-RPC 2.0 initialize request: %+v", initializeRequest)
	}
	if initializeRequest.Params.ProtocolVersion != mcpPinnedProtocolVersion {
		t.Fatalf("fixture protocolVersion = %q, want the pinned literal %q", initializeRequest.Params.ProtocolVersion, mcpPinnedProtocolVersion)
	}

	req, err := mcpInitializePostRequest(s.ts.URL+"/mcp", fixture)
	if err != nil {
		t.Fatalf("failed to build initialize request: %v", err)
	}
	resp, body := s.do(t, req)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, body)
	}
	assertJSONMediaType(t, resp)

	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize response did not return a non-empty Mcp-Session-Id header")
	}

	var envelope mcpContractJSONRPC
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("initialize response is not valid JSON: %v (body=%s)", err, body)
	}
	assertJSONRPCSuccessEnvelope(t, &envelope, fixture)

	var result struct {
		ProtocolVersion string          `json:"protocolVersion"`
		Capabilities    json.RawMessage `json:"capabilities"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("initialize result is not valid JSON: %v (result=%s)", err, envelope.Result)
	}

	if result.ProtocolVersion != mcpPinnedProtocolVersion {
		t.Fatalf("negotiated protocolVersion = %q, want exactly the pinned literal %q; a mismatch means the mcp-go dependency's protocol support changed and this contract plus docs/mcp-compatibility.md must be consciously updated",
			result.ProtocolVersion, mcpPinnedProtocolVersion)
	}

	if result.ServerInfo.Name != "AKS MCP" {
		t.Errorf("serverInfo.name = %q, want %q", result.ServerInfo.Name, "AKS MCP")
	}
	if result.ServerInfo.Version != version.GetVersion() {
		t.Errorf("serverInfo.version = %q, want the build version %q", result.ServerInfo.Version, version.GetVersion())
	}

	// Capability contract: decode the advertised capabilities both as a map
	// (to pin the exact key set) and field-by-field (for precise failure
	// messages).
	var capabilityFields map[string]json.RawMessage
	if err := json.Unmarshal(result.Capabilities, &capabilityFields); err != nil {
		t.Fatalf("capabilities is not a JSON object: %v (capabilities=%s)", err, result.Capabilities)
	}

	gotKeys := make([]string, 0, len(capabilityFields))
	for key := range capabilityFields {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	wantKeys := []string{"logging", "prompts", "resources", "tools"}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("advertised capability keys = %v, want exactly %v; any addition or removal here is a contract change that must update this suite and docs/mcp-compatibility.md",
			gotKeys, wantKeys)
	}

	// sampling, roots, elicitation, completions, tasks (and any
	// experimental/extensions block) must not be advertised by this server.
	for _, absent := range []string{"sampling", "roots", "elicitation", "completions", "tasks", "experimental", "extensions"} {
		if raw, ok := capabilityFields[absent]; ok {
			t.Errorf("capabilities.%s must not be advertised, got %s", absent, raw)
		}
	}

	var capabilities struct {
		Resources *struct {
			Subscribe   bool `json:"subscribe"`
			ListChanged bool `json:"listChanged"`
		} `json:"resources"`
		Prompts *struct {
			ListChanged bool `json:"listChanged"`
		} `json:"prompts"`
		Tools *struct {
			ListChanged bool `json:"listChanged"`
		} `json:"tools"`
		Logging json.RawMessage `json:"logging"`
	}
	if err := json.Unmarshal(result.Capabilities, &capabilities); err != nil {
		t.Fatalf("failed to decode capabilities fields: %v", err)
	}

	if capabilities.Resources == nil {
		t.Error("capabilities.resources must be advertised (server is built with WithResourceCapabilities(true, true))")
	} else {
		if !capabilities.Resources.Subscribe {
			t.Error("capabilities.resources.subscribe = false, want true")
		}
		if !capabilities.Resources.ListChanged {
			t.Error("capabilities.resources.listChanged = false, want true")
		}
	}
	if capabilities.Prompts == nil {
		t.Error("capabilities.prompts must be advertised (server is built with WithPromptCapabilities(true))")
	} else if !capabilities.Prompts.ListChanged {
		t.Error("capabilities.prompts.listChanged = false, want true")
	}
	// The server is constructed without WithToolCapabilities, but tools are
	// registered even in token-auth-only mode (the kubectl component always
	// registers), which makes mcp-go advertise tools with listChanged=true.
	// Note that capability advertisement is not proof of a useful registered
	// tool set; tool schemas and invocation are not covered by this slice.
	if capabilities.Tools == nil {
		t.Error("capabilities.tools must be advertised (tools are registered during Initialize)")
	} else if !capabilities.Tools.ListChanged {
		t.Error("capabilities.tools.listChanged = false, want true (implicitly registered with listChanged when tools are added)")
	}
	if len(bytes.TrimSpace(capabilities.Logging)) == 0 || bytes.Equal(bytes.TrimSpace(capabilities.Logging), []byte("null")) {
		t.Error("capabilities.logging must be advertised (server is built with WithLogging())")
	}
}

// TestMCPPingSessionHeaderContract initializes a session with the pinned
// fixture, then sends the ping fixture reusing the returned session ID and
// the negotiated Mcp-Protocol-Version header. It asserts the ping succeeds
// on the JSON-RPC layer and — via the recording boundary inside the mounted
// route — that the exact negotiated protocol-version header reached the
// actual Streamable HTTP handler. A missing, misspelled, or wrong-valued
// header on the outgoing request fails the observation assertion. It does
// not claim mcp-go rejects bad headers server-side; that enforcement is
// deferred (see docs/mcp-compatibility.md).
func TestMCPPingSessionHeaderContract(t *testing.T) {
	s := startMCPContractServer(t)

	sessionID := s.initialize(t)

	pingFixture := loadContractFixture(t, "mcp_ping.json")

	req, err := mcpSessionPostRequest(s.ts.URL+"/mcp", pingFixture, sessionID, mcpPinnedProtocolVersion)
	if err != nil {
		t.Fatalf("failed to build ping request: %v", err)
	}
	resp, body := s.do(t, req)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ping status = %d, want %d (body=%s)", resp.StatusCode, http.StatusOK, body)
	}
	assertJSONMediaType(t, resp)

	var envelope mcpContractJSONRPC
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("ping response is not valid JSON: %v (body=%s)", err, body)
	}
	assertJSONRPCSuccessEnvelope(t, &envelope, pingFixture)
	if trimmed := bytes.TrimSpace(envelope.Result); string(trimmed) != "{}" {
		t.Errorf("ping result = %s, want an empty result object {}", trimmed)
	}

	// Observation assertion at the route boundary: prove the request that
	// reached the mounted Streamable HTTP transport carried exactly the
	// negotiated protocol-version header and the initialize-issued session
	// ID. If the client-side header setup were dropped, misspelled, or given
	// the wrong value, this fails even though the server would still answer
	// the ping (mcp-go v0.54.1 does not enforce the header).
	var pingObserved *recordedMCPRequest
	observedRequests := s.recorder.snapshot()
	for i := range observedRequests {
		if observedRequests[i].SessionID == sessionID {
			pingObserved = &observedRequests[i]
			break
		}
	}
	if pingObserved == nil {
		t.Fatalf("no request carrying session ID %q was observed at the mounted MCP transport boundary", sessionID)
	}
	if pingObserved.Method != http.MethodPost || pingObserved.Path != "/mcp" {
		t.Errorf("observed request = %s %s, want POST /mcp", pingObserved.Method, pingObserved.Path)
	}
	if pingObserved.ProtocolVersion != mcpPinnedProtocolVersion {
		t.Fatalf("the request that reached the Streamable HTTP handler carried Mcp-Protocol-Version %q, want exactly %q",
			pingObserved.ProtocolVersion, mcpPinnedProtocolVersion)
	}
}

// TestMCPPinnedProtocolHeaderBuilderContract pins the client-side request
// builder: a post-initialization request cannot be built with a missing,
// stale, or unknown protocol version, nor without a session ID. The stale
// "2025-06-18" value is called out explicitly because it was the baseline of
// the closed PR #17 attempt; it is not the version this dependency-era
// contract is pinned to.
func TestMCPPinnedProtocolHeaderBuilderContract(t *testing.T) {
	fixture := loadContractFixture(t, "mcp_ping.json")
	const sessionID = "mcp-2f0c0f6e-1111-4222-8333-444455556666"

	cases := []struct {
		name            string
		sessionID       string
		protocolVersion string
	}{
		{name: "missing protocol version", sessionID: sessionID, protocolVersion: ""},
		{name: "stale 2025-06-18 version from the closed PR #17 baseline", sessionID: sessionID, protocolVersion: "2025-06-18"},
		{name: "unknown version", sessionID: sessionID, protocolVersion: "2099-01-01"},
		{name: "missing session ID", sessionID: "", protocolVersion: mcpPinnedProtocolVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := mcpSessionPostRequest("http://contract.invalid/mcp", fixture, tc.sessionID, tc.protocolVersion)
			if err == nil {
				t.Fatal("expected the contract request builder to refuse a request without the pinned post-initialization headers")
			}
			if req != nil {
				t.Fatalf("expected no request to be returned on refusal, got %v", req)
			}
		})
	}

	// Positive control: with the pinned version the builder produces the
	// full post-initialization header set.
	req, err := mcpSessionPostRequest("http://contract.invalid/mcp", fixture, sessionID, mcpPinnedProtocolVersion)
	if err != nil {
		t.Fatalf("expected the builder to accept the pinned protocol version %q: %v", mcpPinnedProtocolVersion, err)
	}
	if got := req.Header.Get("Mcp-Protocol-Version"); got != mcpPinnedProtocolVersion {
		t.Errorf("Mcp-Protocol-Version = %q, want %q", got, mcpPinnedProtocolVersion)
	}
	if got := req.Header.Get("Mcp-Session-Id"); got != sessionID {
		t.Errorf("Mcp-Session-Id = %q, want %q", got, sessionID)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := req.Header.Get("Accept"); got != "application/json, text/event-stream" {
		t.Errorf("Accept = %q, want application/json, text/event-stream", got)
	}
}
