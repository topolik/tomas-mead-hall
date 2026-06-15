// Package control runs LLP's handshake endpoint over a Unix domain socket. The
// socket lives in a 0700 owner-only directory (the security gate); an agent
// connects at its startup, names itself, and receives a fresh in-memory session
// token. The peer's credentials (uid/pid) are captured via SO_PEERCRED and
// logged; strict same-uid enforcement is optional (see Serve). No token ever
// touches env, disk, or /proc.
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/topolik/llp-llm-proxy/internal/auth"
)

type credKeyT struct{}

// peerCred reads the connecting peer's credentials from a Unix socket.
func peerCred(uc *net.UnixConn) (*syscall.Ucred, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *syscall.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	return cred, cerr
}

func connContext(ctx context.Context, c net.Conn) context.Context {
	if uc, ok := c.(*net.UnixConn); ok {
		if cred, err := peerCred(uc); err == nil {
			return context.WithValue(ctx, credKeyT{}, cred)
		}
	}
	return ctx
}

func registerHandler(store *auth.Store, requireSameUID bool) http.HandlerFunc {
	selfUID := os.Getuid()
	return func(w http.ResponseWriter, r *http.Request) {
		cred, _ := r.Context().Value(credKeyT{}).(*syscall.Ucred)

		var body struct {
			Agent string `json:"agent"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		agent := strings.TrimSpace(body.Agent)
		if agent == "" {
			http.Error(w, "agent required", http.StatusBadRequest)
			return
		}

		peer := "uid=? pid=?"
		if cred != nil {
			peer = fmt.Sprintf("uid=%d pid=%d", cred.Uid, cred.Pid)
			if requireSameUID && int(cred.Uid) != selfUID {
				log.Printf("control: REJECT register agent=%q peer %s (require_same_uid; self uid=%d)", agent, peer, selfUID)
				http.Error(w, "peer uid not permitted", http.StatusForbidden)
				return
			}
		}

		tok, err := store.Issue(agent)
		if err != nil {
			http.Error(w, "could not issue token", http.StatusInternalServerError)
			return
		}
		log.Printf("control: issued session token to agent=%q peer %s", agent, peer)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": tok, "agent": agent})
	}
}

// Serve starts the control server on a Unix socket at socketPath (its parent dir
// is created 0700) and returns a closer. requireSameUID, when true, rejects
// handshakes whose peer uid differs from LLP's (host-only deployments). Closing
// the returned io.Closer stops the server and removes the socket.
func Serve(socketPath string, store *auth.Store, requireSameUID bool) (io.Closer, error) {
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	_ = os.Remove(socketPath) // clear a stale socket from a previous run
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", socketPath, err)
	}
	// The 0700 dir is the access gate; 0666 on the socket lets a bind-mounted
	// container connect as its own uid without breaking its volume perms.
	if err := os.Chmod(socketPath, 0o666); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod %s: %w", socketPath, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", registerHandler(store, requireSameUID))
	srv := &http.Server{Handler: mux, ConnContext: connContext}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("control: serve: %v", err)
		}
	}()
	return srv, nil // http.Server.Close() closes the listener, unlinking the socket
}
