package llm

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFromEnv(t *testing.T) {
	os.Setenv("LLP_URL", "http://test:9090")
	os.Setenv("LLP_SOCKET", "/tmp/test.sock")
	os.Setenv("LLP_MODEL", "test-model")
	defer func() {
		os.Unsetenv("LLP_URL")
		os.Unsetenv("LLP_SOCKET")
		os.Unsetenv("LLP_MODEL")
	}()

	c := NewFromEnv()
	if c.URL != "http://test:9090" {
		t.Errorf("URL = %q, want %q", c.URL, "http://test:9090")
	}
	if c.Socket != "/tmp/test.sock" {
		t.Errorf("Socket = %q, want %q", c.Socket, "/tmp/test.sock")
	}
	if c.Model != "test-model" {
		t.Errorf("Model = %q, want %q", c.Model, "test-model")
	}
}

func TestAvailable(t *testing.T) {
	if (&Client{}).Available() {
		t.Error("empty client should not be available")
	}
	if !(&Client{URL: "http://x"}).Available() {
		t.Error("client with URL should be available")
	}
}

func TestEffectiveModel(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"", "gml-analyze"},
		{"custom", "custom"},
	}
	for _, c := range cases {
		got := (&Client{Model: c.model}).effectiveModel()
		if got != c.want {
			t.Errorf("effectiveModel(%q) = %q, want %q", c.model, got, c.want)
		}
	}
}

func TestEffectiveSocket(t *testing.T) {
	c := &Client{Socket: "/custom/sock"}
	if got := c.effectiveSocket(); got != "/custom/sock" {
		t.Errorf("effectiveSocket = %q, want /custom/sock", got)
	}

	c2 := &Client{}
	got := c2.effectiveSocket()
	home, _ := os.UserHomeDir()
	want := home + "/.llp/control.sock"
	if got != want {
		t.Errorf("effectiveSocket default = %q, want %q", got, want)
	}
}

func TestLLPCall_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			body, _ := io.ReadAll(r.Body)
			var req map[string]interface{}
			json.Unmarshal(body, &req)

			if req["model"] != "test-model" {
				t.Errorf("model = %v, want test-model", req["model"])
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "hello world"}},
				},
			})
			return
		}
		t.Errorf("unexpected path: %s", r.URL.Path)
	}))
	defer ts.Close()

	c := &Client{URL: ts.URL, Model: "test-model"}
	// bypass handshake by injecting token directly — test llpCall via Call
	// We can't easily test through Call without a unix socket, so test the
	// HTTP path parsing by using a mock server that doesn't need auth
	result, err := c.llpCallWithToken("fake-token", "test prompt")
	if err != nil {
		t.Fatalf("llpCall: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

func TestLLPCall_EmptyChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{}})
	}))
	defer ts.Close()

	c := &Client{URL: ts.URL, Model: "test"}
	_, err := c.llpCallWithToken("fake", "prompt")
	if err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Errorf("expected 'empty choices' error, got: %v", err)
	}
}

func TestLLPCall_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer ts.Close()

	c := &Client{URL: ts.URL, Model: "test"}
	_, err := c.llpCallWithToken("fake", "prompt")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 error, got: %v", err)
	}
}

func TestCliCall_UnknownModel(t *testing.T) {
	c := &Client{}
	_, err := c.cliCall("unknown-model", "prompt")
	if err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Errorf("expected 'unknown model' error, got: %v", err)
	}
}

func TestHandshake_Success(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/register" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"agent":"gml"`) {
			t.Errorf("unexpected body: %s", body)
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "test-token-123"})
	})}
	go srv.Serve(ln)
	defer srv.Close()

	c := &Client{Socket: sockPath}
	token, err := c.handshake()
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if token != "test-token-123" {
		t.Errorf("token = %q, want test-token-123", token)
	}
}

func TestHandshake_EmptyToken(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"token": ""})
	})}
	go srv.Serve(ln)
	defer srv.Close()

	c := &Client{Socket: sockPath}
	_, err = c.handshake()
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Errorf("expected 'empty token' error, got: %v", err)
	}
}

func TestCall_RoutesToLLP(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
	})}
	go srv.Serve(ln)
	defer srv.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth = %q, want 'Bearer tok'", r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "LLP response"}},
			},
		})
	}))
	defer ts.Close()

	c := &Client{URL: ts.URL, Socket: sockPath, Model: "test-m"}
	result, err := c.Call("ignored-when-llp-available", "hello")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result != "LLP response" {
		t.Errorf("result = %q, want 'LLP response'", result)
	}
}
