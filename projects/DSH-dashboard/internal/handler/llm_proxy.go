package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"dsh/internal/llpclient"
)

var defaultModelSuggestions = []string{"auto", "gemini", "claude", "openllm", "gml-analyze"}

// renderLLMProxy fetches the proxy's health, usage and recent-requests panels
// and renders the LLP tab. playground carries optional playground form state
// (PgModel, PgPrompt, PgResult, PgServedBy, PgError) merged into the template data.
func (h *UIHandler) renderLLMProxy(w http.ResponseWriter, r *http.Request, playground map[string]any) {
	c := llpclient.New(h.LLPURL, h.LLPSocket, "dsh")
	ctx := r.Context()

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	data := map[string]any{
		"Configured":  c.Configured(),
		"BaseURL":     h.LLPURL,
		"CSRFToken":   h.csrfToken(r),
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
		// default playground model suggestions; replaced with live impl names below
		"Suggestions": []string{"auto", "gemini", "claude", "gml-analyze"},
	}

	if c.Configured() {
		if health, err := c.Health(ctx); err != nil {
			data["HealthErr"] = err.Error()
		} else {
			data["Health"] = health
			sugg := make([]string, 0, len(health.Impls)+2)
			for _, im := range health.Impls {
				sugg = append(sugg, im.Name)
			}
			data["Suggestions"] = append(sugg, "auto", "gml-analyze")
		}
		if usage, err := c.Usage(ctx); err != nil {
			data["UsageErr"] = err.Error()
		} else {
			data["Usage"] = usage
		}
		if recent, err := c.Recent(ctx, 50); err != nil {
			data["RecentErr"] = err.Error()
		} else {
			data["Recent"] = recent
		}
	}

	for k, v := range playground {
		data[k] = v
	}
	h.Tmpls.ExecuteTemplate(w, "llm_proxy.html", data)
}

// LLMProxyPage renders the LLP tab (GET /llm-proxy).
func (h *UIHandler) LLMProxyPage(w http.ResponseWriter, r *http.Request) {
	h.renderLLMProxy(w, r, nil)
}

// LLMProxyRun runs a playground turn through the proxy (POST /llm-proxy/run).
// The conversation history rides along in a hidden base64 field (the proxy is
// stateless), so each Send sends the full transcript. For HTMX requests it
// returns just the playground fragment (transcript + form); otherwise the whole tab.
func (h *UIHandler) LLMProxyRun(w http.ResponseWriter, r *http.Request) {
	model := strings.TrimSpace(r.FormValue("model"))
	if model == "" {
		model = "auto"
	}
	history := decodeHistory(r.FormValue("history"))

	// Clear button: reset the conversation.
	if r.FormValue("clear") != "" {
		h.renderPlayground(w, r, model, "", nil, "", "", "")
		return
	}

	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		h.renderPlayground(w, r, model, "", history, "", "", "prompt is required")
		return
	}

	convo := append(history, llpclient.Message{Role: "user", Content: prompt})
	start := time.Now()
	text, served, err := llpclient.New(h.LLPURL, h.LLPSocket, "dsh").Chat(r.Context(), model, convo)
	latency := time.Since(start).Round(100 * time.Millisecond).String()
	if err != nil {
		// keep prior turns; preserve the prompt so the user can retry the failed turn
		h.renderPlayground(w, r, model, prompt, history, "", latency, err.Error())
		return
	}
	convo = append(convo, llpclient.Message{Role: "assistant", Content: text})
	h.renderPlayground(w, r, model, "", convo, served, latency, "")
}

func decodeHistory(b64 string) []llpclient.Message {
	if b64 == "" {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil
	}
	var msgs []llpclient.Message
	if json.Unmarshal(raw, &msgs) != nil {
		return nil
	}
	return msgs
}

// renderPlayground renders the playground (transcript + form). The conversation
// is carried forward in a base64 hidden field. prompt is non-empty only to
// repopulate the box after an error.
func (h *UIHandler) renderPlayground(w http.ResponseWriter, r *http.Request, model, prompt string, messages []llpclient.Message, servedBy, latency, errMsg string) {
	hb := ""
	if len(messages) > 0 {
		if raw, err := json.Marshal(messages); err == nil {
			hb = base64.StdEncoding.EncodeToString(raw)
		}
	}
	pg := map[string]any{
		"PgModel": model, "PgPrompt": prompt, "PgMessages": messages,
		"PgHistoryB64": hb, "PgServedBy": servedBy, "PgLatency": latency, "PgError": errMsg,
	}
	if r.Header.Get("HX-Request") == "true" {
		pg["CSRFToken"] = h.csrfToken(r)
		pg["Suggestions"] = defaultModelSuggestions
		if err := h.Tmpls.ExecuteTemplate(w, "llm_proxy_playground", pg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	h.renderLLMProxy(w, r, pg)
}
