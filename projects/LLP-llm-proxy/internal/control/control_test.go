package control

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/topolik/llp-llm-proxy/internal/auth"
)

func unixClient(sock string) *http.Client {
	return &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}
}

func TestRegisterIssuesToken(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctl.sock")
	store := auth.NewStore()
	closer, err := Serve(sock, store, false)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer closer.Close()

	resp, err := unixClient(sock).Post("http://unix/register", "application/json", strings.NewReader(`{"agent":"dsh"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out struct{ Token, Agent string }
	json.NewDecoder(resp.Body).Decode(&out)
	if out.Agent != "dsh" || out.Token == "" {
		t.Fatalf("bad response: %+v", out)
	}
	if ag, ok := store.Agent(out.Token); !ok || ag != "dsh" {
		t.Fatalf("token not in store: %q %v", ag, ok)
	}
}

func TestRegisterEmptyAgentRejected(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "ctl.sock")
	closer, err := Serve(sock, auth.NewStore(), false)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	defer closer.Close()

	resp, err := unixClient(sock).Post("http://unix/register", "application/json", strings.NewReader(`{"agent":""}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
