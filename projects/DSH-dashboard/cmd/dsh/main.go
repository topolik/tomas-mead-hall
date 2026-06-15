package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-webauthn/webauthn/webauthn"

	webpush "github.com/SherClockHolmes/webpush-go"

	"dsh/internal/auth"
	"dsh/internal/config"
	"dsh/internal/db"
	"dsh/internal/handler"

	"embed"
	"io/fs"
)

//go:embed web/templates/*.html
var templatesFS embed.FS

//go:embed web/static
var staticFS embed.FS

func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	if err := bootstrap(database); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	// CLI subcommand: create-client
	if len(os.Args) >= 2 && os.Args[1] == "create-client" {
		if len(os.Args) < 3 {
			log.Fatalf("usage: dsh create-client <name>")
		}
		clientID, clientSecret, err := auth.CreateOAuth2Client(database, os.Args[2])
		if err != nil {
			log.Fatalf("create-client: %v", err)
		}
		out, _ := json.Marshal(map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
		})
		fmt.Println(string(out))
		os.Exit(0)
	}

	jwtSecret, err := loadOrGenSecret(database, "jwt_secret")
	if err != nil {
		log.Fatalf("jwt secret: %v", err)
	}

	vapidPub, vapidPriv, err := loadOrGenVAPID(database)
	if err != nil {
		log.Fatalf("vapid keys: %v", err)
	}

	waMap, err := auth.NewWebAuthnMap(cfg.Origins())
	if err != nil {
		log.Fatalf("webauthn: %v", err)
	}

	// Always construct the setup token so authenticated users can mint a fresh
	// device-enrollment link on demand (Passkeys → Add a new device). The boot
	// value below expires 10 min after start; Regenerate() resets its clock.
	b := make([]byte, 16)
	rand.Read(b)
	tokenFile := filepath.Join(filepath.Dir(cfg.DBPath), "setup-token")
	setupToken := handler.NewSetupToken(hex.EncodeToString(b), tokenFile)

	tmpls, err := loadTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	mux := buildMux(database, tmpls, waMap, jwtSecret, vapidPub, vapidPriv, cfg, setupToken)

	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		log.Printf("DSH starting on :%s (HTTPS, origin: %s)", cfg.Port, cfg.Origin)
		if err := http.ListenAndServeTLS(":"+cfg.Port, cfg.TLSCert, cfg.TLSKey, mux); err != nil {
			log.Fatal(err)
		}
	} else {
		log.Printf("DSH starting on :%s (origin: %s)", cfg.Port, cfg.Origin)
		if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
			log.Fatal(err)
		}
	}
}

func buildMux(database *sql.DB, tmpls *template.Template, waMap map[string]*webauthn.WebAuthn, jwtSecret, vapidPub, vapidPriv string, cfg *config.Config, setupToken *handler.SetupToken) *http.ServeMux {
	mux := http.NewServeMux()

	defaultRPID := ""
	if origins := cfg.Origins(); len(origins) > 0 {
		defaultRPID = auth.RPIDFromOrigin(origins[0])
	}
	authH := &handler.AuthHandler{
		DB:             database,
		Tmpls:          tmpls,
		WAMap:          waMap,
		JWTSec:         jwtSecret,
		Issuer:         "DSH Dashboard",
		SetupToken:     setupToken,
		ExternalOrigin: cfg.ExternalOrigin(),
		DefaultRPID:    defaultRPID,
	}
	uiH := &handler.UIHandler{DB: database, Tmpls: tmpls, PMPath: cfg.PMPath, TodoPath: cfg.TodoPath, LLPURL: cfg.LLPURL, LLPSocket: cfg.LLPSocket}
	apiH := &handler.APIHandler{
		DB:              database,
		Version:         "0.1.0",
		TodoPath:        cfg.TodoPath,
		VAPIDPublicKey:  vapidPub,
		VAPIDPrivateKey: vapidPriv,
		VAPIDContact:    cfg.VAPIDContact,
	}
	pushH := &handler.PushHandler{DB: database, VAPIDPublicKey: vapidPub}

	// Static files
	staticSub, _ := fs.Sub(staticFS, "web/static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Service worker at root for root scope
	swData, _ := staticFS.ReadFile("web/static/sw.js")
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(swData)
	})

	// Favicon at root: browsers auto-request /favicon.ico on every page, so
	// serving here covers the whole app with no per-template <link> tags.
	faviconData, _ := staticFS.ReadFile("web/static/favicon.svg")
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(faviconData)
	})

	// CA cert download (public, for phone trust setup)
	if cfg.TLSCert != "" {
		caPath := filepath.Join(filepath.Dir(cfg.TLSCert), "ca.crt")
		mux.HandleFunc("GET /ca.crt", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-x509-ca-cert")
			w.Header().Set("Content-Disposition", "attachment; filename=dsh-ca.crt")
			http.ServeFile(w, r, caPath)
		})
	}

	// Health (public)
	mux.HandleFunc("GET /api/v1/health", apiH.Health)

	// OAuth2 token (public)
	mux.HandleFunc("POST /oauth/token", authH.OAuth2Token)

	// First-run setup (public, only when no passkeys registered)
	mux.HandleFunc("GET /setup", authH.SetupPage)
	mux.HandleFunc("GET /setup/passkey/begin", authH.SetupPasskeyBegin)
	mux.HandleFunc("POST /setup/passkey/finish", authH.SetupPasskeyFinish)

	// Login (passkey only, public)
	mux.HandleFunc("GET /login", authH.LoginPage)
	mux.HandleFunc("GET /logout", authH.Logout)

	// Passkey login ceremony (public)
	mux.HandleFunc("GET /auth/passkey/login/begin", authH.PasskeyLoginBegin)
	mux.HandleFunc("POST /auth/passkey/login/finish", authH.PasskeyLoginFinish)

	// Passkey settings (session required)
	mux.HandleFunc("GET /settings/passkeys",
		handler.RequireSession(database, jwtSecret, authH.PasskeysPage))
	mux.HandleFunc("POST /settings/passkeys/enroll",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, authH.EnrollDevice)))
	mux.HandleFunc("GET /auth/passkey/register/begin",
		handler.RequireSession(database, jwtSecret, authH.PasskeyRegisterBegin))
	mux.HandleFunc("POST /auth/passkey/register/finish",
		handler.RequireSession(database, jwtSecret, authH.PasskeyRegisterFinish))
	mux.HandleFunc("POST /auth/passkey/remove",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, authH.PasskeyRemove)))

	// Dashboard UI (authenticated)
	mux.HandleFunc("GET /{$}", handler.RequireSession(database, jwtSecret, uiH.Root))
	mux.HandleFunc("GET /todo", handler.RequireSession(database, jwtSecret, uiH.TodoPage))
	mux.HandleFunc("POST /todo",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.TodoAdd)))
	mux.HandleFunc("POST /todo/{id}",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.TodoUpdate)))
	mux.HandleFunc("GET /todo/{id}/edit",
		handler.RequireSession(database, jwtSecret, uiH.TodoEditPage))
	mux.HandleFunc("POST /todo/{id}/edit",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.TodoEditSubmit)))
	mux.HandleFunc("POST /todo/{id}/delete",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.TodoDelete)))
	mux.HandleFunc("POST /todo/bulk",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.TodoBulk)))

	mux.HandleFunc("GET /projects", handler.RequireSession(database, jwtSecret, uiH.ProjectsPage))
	mux.HandleFunc("GET /projects/{code}", handler.RequireSession(database, jwtSecret, uiH.ProjectDetailPage))

	mux.HandleFunc("GET /llm-proxy", handler.RequireSession(database, jwtSecret, uiH.LLMProxyPage))
	mux.HandleFunc("POST /llm-proxy/run",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.LLMProxyRun)))

	mux.HandleFunc("GET /notifications", handler.RequireSession(database, jwtSecret, uiH.NotificationsPage))
	mux.HandleFunc("POST /notifications/bulk-dismiss",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.NotificationBulkDismiss)))
	mux.HandleFunc("POST /notifications/{id}/dismiss",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.NotificationDismiss)))
	mux.HandleFunc("POST /notifications/{id}/restore",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.NotificationRestore)))
	mux.HandleFunc("POST /notifications/{id}/delete",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.NotificationDelete)))
	mux.HandleFunc("POST /notifications/{id}/comment",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.NotificationComment)))

	mux.HandleFunc("GET /plans", handler.RequireSession(database, jwtSecret, uiH.PlansPage))
	mux.HandleFunc("POST /plans/{id}/decide",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.PlanDecide)))
	mux.HandleFunc("GET /plans/{id}/edit",
		handler.RequireSession(database, jwtSecret, uiH.PlanEditPage))
	mux.HandleFunc("POST /plans/{id}/edit",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.PlanEditSave)))
	mux.HandleFunc("POST /plans/{id}/delete",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.PlanDelete)))

	mux.HandleFunc("GET /threads", handler.RequireSession(database, jwtSecret, uiH.ThreadsPage))
	mux.HandleFunc("GET /threads/{id}", handler.RequireSession(database, jwtSecret, uiH.ThreadDetailPage))
	mux.HandleFunc("POST /threads",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.ThreadCreateUI)))
	mux.HandleFunc("POST /threads/{id}/reply",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.ThreadReplyUI)))
	mux.HandleFunc("POST /threads/{id}/status",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.ThreadStatusUI)))

	mux.HandleFunc("GET /admin/clients", handler.RequireSession(database, jwtSecret, uiH.AdminClientsPage))
	mux.HandleFunc("GET /admin/audit", handler.RequireSession(database, jwtSecret, uiH.AdminAuditPage))
	mux.HandleFunc("POST /admin/clients",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.AdminClientCreate)))
	mux.HandleFunc("POST /admin/clients/{id}/revoke",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, uiH.AdminClientRevoke)))

	// Push notifications (session protected)
	mux.HandleFunc("GET /push/vapid-key",
		handler.RequireSession(database, jwtSecret, pushH.VAPIDKey))
	mux.HandleFunc("POST /push/subscribe",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, pushH.Subscribe)))
	mux.HandleFunc("POST /push/unsubscribe",
		handler.RequireSession(database, jwtSecret,
			handler.CheckCSRF(database, pushH.Unsubscribe)))

	// API (JWT protected)
	mux.HandleFunc("POST /api/v1/projects", handler.RequireJWT(database, jwtSecret, apiH.UpsertProject))
	mux.HandleFunc("GET /api/v1/projects", handler.RequireJWT(database, jwtSecret, apiH.ListProjects))
	mux.HandleFunc("GET /api/v1/notifications", handler.RequireJWT(database, jwtSecret, apiH.ListNotifications))
	mux.HandleFunc("POST /api/v1/notifications", handler.RequireJWT(database, jwtSecret, apiH.CreateNotification))
	mux.HandleFunc("PATCH /api/v1/notifications/{id}", handler.RequireJWT(database, jwtSecret, apiH.UpdateNotification))
	mux.HandleFunc("GET /api/v1/todos", handler.RequireJWT(database, jwtSecret, apiH.ListTodos))
	mux.HandleFunc("POST /api/v1/todos", handler.RequireJWT(database, jwtSecret, apiH.CreateTodo))
	mux.HandleFunc("GET /api/v1/plans", handler.RequireJWT(database, jwtSecret, apiH.ListPlans))
	mux.HandleFunc("POST /api/v1/plans", handler.RequireJWT(database, jwtSecret, apiH.CreatePlan))
	mux.HandleFunc("PATCH /api/v1/plans/{id}", handler.RequireJWT(database, jwtSecret, apiH.UpdatePlan))
	mux.HandleFunc("DELETE /api/v1/plans/{id}", handler.RequireJWT(database, jwtSecret, apiH.DeletePlan))
	mux.HandleFunc("GET /api/v1/threads", handler.RequireJWT(database, jwtSecret, apiH.ListThreads))
	mux.HandleFunc("POST /api/v1/threads", handler.RequireJWT(database, jwtSecret, apiH.CreateThread))
	mux.HandleFunc("GET /api/v1/threads/{id}", handler.RequireJWT(database, jwtSecret, apiH.GetThread))
	mux.HandleFunc("PATCH /api/v1/threads/{id}", handler.RequireJWT(database, jwtSecret, apiH.UpdateThread))
	mux.HandleFunc("POST /api/v1/threads/{id}/messages", handler.RequireJWT(database, jwtSecret, apiH.PostThreadMessage))

	return mux
}

func loadTemplates() (*template.Template, error) {
	return template.ParseFS(templatesFS, "web/templates/*.html")
}

func bootstrap(database *sql.DB) error {
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if count == 0 {
		b := make([]byte, 32)
		rand.Read(b)
		hash, err := auth.HashPassword(hex.EncodeToString(b))
		if err != nil {
			return err
		}
		_, err = database.Exec(`INSERT INTO users(username, password_hash) VALUES('admin',?)`, hash)
		if err != nil {
			return err
		}
		log.Printf("bootstrap: user 'admin' created — register your passkey at /setup")
	}

	// Seed DSH project (day-one dogfood)
	_, err := database.Exec(`
		INSERT INTO projects(code, name, status, priority, lead, current_phase, last_updated)
		VALUES('DSH','Dashboard','Planning','Q2','Developer','Planning', date('now'))
		ON CONFLICT(code) DO NOTHING`)
	return err
}

func loadOrGenVAPID(database *sql.DB) (pub, priv string, err error) {
	err = database.QueryRow(`SELECT value FROM config WHERE key='vapid_public_key'`).Scan(&pub)
	if errors.Is(err, sql.ErrNoRows) {
		priv, pub, err = webpush.GenerateVAPIDKeys()
		if err != nil {
			return "", "", err
		}
		if _, err = database.Exec(`INSERT INTO config(key,value) VALUES('vapid_public_key',?)`, pub); err != nil {
			return "", "", err
		}
		if _, err = database.Exec(`INSERT INTO config(key,value) VALUES('vapid_private_key',?)`, priv); err != nil {
			return "", "", err
		}
		log.Printf("bootstrap: generated VAPID keys")
		return pub, priv, nil
	}
	if err != nil {
		return "", "", err
	}
	err = database.QueryRow(`SELECT value FROM config WHERE key='vapid_private_key'`).Scan(&priv)
	return pub, priv, err
}

func loadOrGenSecret(database *sql.DB, key string) (string, error) {
	var val string
	err := database.QueryRow(`SELECT value FROM config WHERE key=?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		b := make([]byte, 32)
		rand.Read(b)
		val = hex.EncodeToString(b)
		_, err = database.Exec(`INSERT INTO config(key,value) VALUES(?,?)`, key, val)
		if err != nil {
			return "", err
		}
		log.Printf("bootstrap: generated %s", key)
	} else if err != nil {
		return "", err
	}
	return val, nil
}
