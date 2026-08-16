package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/aks-mcp/internal/azcli"
	"github.com/Azure/aks-mcp/internal/ctx"
	"github.com/Azure/aks-mcp/internal/version"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const mcpContractProtocolVersion = "2025-06-18"

type mcpContractResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

type recordingHandler struct {
	next                http.Handler
	lastProtocolVersion string
	requests            int
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.requests++
	h.lastProtocolVersion = r.Header.Get("Mcp-Protocol-Version")
	h.next.ServeHTTP(w, r)
}

// mcpContractClient is deliberately small: it represents only a negotiated
// Streamable HTTP exchange, not a general-purpose MCP client.
type mcpContractClient struct {
	baseURL    string
	httpClient *http.Client
	sessionID  string
	protocol   string
}

func (c *mcpContractClient) ping(ctx context.Context, body []byte) (*http.Response, error) {
	if c.protocol != mcpContractProtocolVersion {
		return nil, fmt.Errorf("unsupported negotiated protocol version %q", c.protocol)
	}
	if c.sessionID == "" {
		return nil, fmt.Errorf("missing MCP session ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Mcp-Session-Id", c.sessionID)
	req.Header.Set("Mcp-Protocol-Version", c.protocol)
	return c.httpClient.Do(req)
}

func TestMCPStreamableHTTPContract(t *testing.T) {
	initialize := readMCPContractFixture(t, "mcp_initialize_2025-06-18.json")
	ping := readMCPContractFixture(t, "mcp_ping.json")

	cfg := createTestConfig("readonly", []string{})
	cfg.Transport = "streamable-http"
	cfg.Host = "127.0.0.1"
	cfg.TokenAuthOnly = true
	service := NewService(cfg, WithAzCliProcFactory(func(int) azcli.Proc { return &fakeProc{} }))
	if err := service.Initialize(); err != nil {
		t.Fatalf("initialize service: %v", err)
	}

	customServer := service.createCustomHTTPServerWithHelp404("127.0.0.1:0")
	streamableServer := mcpserver.NewStreamableHTTPServer(
		service.mcpServer,
		mcpserver.WithStreamableHTTPServer(customServer),
		mcpserver.WithHTTPContextFunc(func(c context.Context, r *http.Request) context.Context {
			if token := r.Header.Get("X-Azure-Token"); token != "" {
				c = context.WithValue(c, ctx.AzureTokenKey, token)
			}
			return c
		}),
	)
	mux := customServer.Handler.(*http.ServeMux)
	service.installStreamableMCPHandler(mux, streamableServer)
	recorder := &recordingHandler{next: customServer.Handler}
	httpServer := httptest.NewServer(recorder)
	defer httpServer.Close()

	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/mcp", bytes.NewReader(initialize))
	if err != nil {
		t.Fatalf("create initialize request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatalf("send initialize request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("initialize content type = %q, want application/json", contentType)
	}
	sessionID := response.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize response did not include Mcp-Session-Id")
	}

	var payload mcpContractResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if payload.JSONRPC != "2.0" {
		t.Errorf("JSON-RPC version = %q, want 2.0", payload.JSONRPC)
	}
	if string(payload.ID) != `"contract-initialize-1"` {
		t.Errorf("response ID = %s, want fixture ID", payload.ID)
	}
	assertMCPInitializeContract(t, payload.Result)

	client := mcpContractClient{baseURL: httpServer.URL, httpClient: httpServer.Client(), sessionID: sessionID, protocol: mcpContractProtocolVersion}
	pingResponse, err := client.ping(context.Background(), ping)
	if err != nil {
		t.Fatalf("send ping: %v", err)
	}
	defer pingResponse.Body.Close()
	if pingResponse.StatusCode != http.StatusOK {
		t.Fatalf("ping status = %d, want %d", pingResponse.StatusCode, http.StatusOK)
	}
	if recorder.lastProtocolVersion != mcpContractProtocolVersion {
		t.Errorf("protocol header at handler = %q, want %q", recorder.lastProtocolVersion, mcpContractProtocolVersion)
	}

	for _, protocol := range []string{"", "2024-11-05"} {
		t.Run("rejects "+protocol, func(t *testing.T) {
			before := recorder.requests
			client.protocol = protocol
			if _, err := client.ping(context.Background(), ping); err == nil {
				t.Fatal("ping accepted an unnegotiated protocol version")
			}
			if recorder.requests != before {
				t.Fatal("unnegotiated ping reached handler")
			}
		})
	}
}

func assertMCPInitializeContract(t *testing.T, result json.RawMessage) {
	t.Helper()
	var decoded struct {
		ProtocolVersion string                         `json:"protocolVersion"`
		ServerInfo      struct{ Name, Version string } `json:"serverInfo"`
		Capabilities    map[string]json.RawMessage     `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if decoded.ProtocolVersion != mcpContractProtocolVersion {
		t.Errorf("protocol version = %q, want %q", decoded.ProtocolVersion, mcpContractProtocolVersion)
	}
	if decoded.ServerInfo.Name != "AKS MCP" || decoded.ServerInfo.Version != version.GetVersion() {
		t.Errorf("server info = %+v, want AKS MCP/%s", decoded.ServerInfo, version.GetVersion())
	}
	for _, capability := range []string{"resources", "prompts", "logging"} {
		if _, ok := decoded.Capabilities[capability]; !ok {
			t.Errorf("missing advertised %s capability", capability)
		}
	}
	for _, capability := range []string{"sampling", "roots", "elicitation", "completions", "tasks"} {
		if _, ok := decoded.Capabilities[capability]; ok {
			t.Errorf("unexpected advertised %s capability", capability)
		}
	}
	var resources, prompts map[string]json.RawMessage
	if err := json.Unmarshal(decoded.Capabilities["resources"], &resources); err != nil || resources["subscribe"] == nil || resources["listChanged"] == nil {
		t.Errorf("resources capability = %s, want subscribe and listChanged", decoded.Capabilities["resources"])
	}
	if err := json.Unmarshal(decoded.Capabilities["prompts"], &prompts); err != nil || prompts["listChanged"] == nil {
		t.Errorf("prompts capability = %s, want listChanged", decoded.Capabilities["prompts"])
	}
}

func readMCPContractFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}
