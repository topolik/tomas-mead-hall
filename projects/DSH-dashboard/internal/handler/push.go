package handler

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type PushHandler struct {
	DB             *sql.DB
	VAPIDPublicKey string
}

func (h *PushHandler) VAPIDKey(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"key": h.VAPIDPublicKey})
}

func (h *PushHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		jsonError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		jsonError(w, http.StatusBadRequest, "endpoint and keys required")
		return
	}

	_, err := h.DB.Exec(
		`INSERT INTO push_subscriptions(user_id, endpoint, key_p256dh, key_auth) VALUES(?,?,?,?)
		 ON CONFLICT(endpoint) DO UPDATE SET key_p256dh=excluded.key_p256dh, key_auth=excluded.key_auth`,
		userID, body.Endpoint, body.Keys.P256dh, body.Keys.Auth,
	)
	if err != nil {
		log.Printf("push subscribe: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (h *PushHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Endpoint == "" {
		jsonError(w, http.StatusBadRequest, "endpoint required")
		return
	}

	h.DB.Exec(`DELETE FROM push_subscriptions WHERE endpoint=?`, body.Endpoint)
	jsonOK(w, map[string]any{"ok": true})
}

func SendWebPush(db *sql.DB, vapidPub, vapidPriv, contact string, title, body, url string) {
	rows, err := db.Query(`SELECT endpoint, key_p256dh, key_auth FROM push_subscriptions`)
	if err != nil {
		log.Printf("push query: %v", err)
		return
	}
	defer rows.Close()

	payload, _ := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"url":   url,
	})

	for rows.Next() {
		var endpoint, p256dh, auth string
		if err := rows.Scan(&endpoint, &p256dh, &auth); err != nil {
			continue
		}

		sub := &webpush.Subscription{
			Endpoint: endpoint,
			Keys: webpush.Keys{
				P256dh: p256dh,
				Auth:   auth,
			},
		}

		resp, err := webpush.SendNotification(payload, sub, &webpush.Options{
			Subscriber:      contact,
			VAPIDPublicKey:  vapidPub,
			VAPIDPrivateKey: vapidPriv,
			TTL:             86400,
		})
		if err != nil {
			log.Printf("push send to %s: %v", endpoint[:40], err)
			continue
		}
		resp.Body.Close()

		log.Printf("push: %s... status=%d", endpoint[:60], resp.StatusCode)
		if resp.StatusCode == 410 || resp.StatusCode == 404 {
			db.Exec(`DELETE FROM push_subscriptions WHERE endpoint=?`, endpoint)
			log.Printf("push: removed stale subscription")
		}
	}
}
