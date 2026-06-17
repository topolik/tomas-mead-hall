package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	URL    string
	Socket string
	Model  string
}

func NewFromEnv() *Client {
	return &Client{
		URL:    os.Getenv("LLP_URL"),
		Socket: os.Getenv("LLP_SOCKET"),
		Model:  os.Getenv("LLP_MODEL"),
	}
}

func (c *Client) Available() bool { return c.URL != "" }

func (c *Client) DisplayName(fallback string) string {
	if c.Available() {
		return "LLP/" + c.effectiveModel()
	}
	return fallback
}

func (c *Client) Call(model, promptText string) (string, error) {
	if c.Available() {
		return c.llpCall(promptText)
	}
	return c.cliCall(model, promptText)
}

func (c *Client) effectiveModel() string {
	if c.Model != "" {
		return c.Model
	}
	return "gml-analyze"
}

func (c *Client) effectiveSocket() string {
	if c.Socket != "" {
		return c.Socket
	}
	home, _ := os.UserHomeDir()
	return home + "/.llp/control.sock"
}

func (c *Client) llpCall(promptText string) (string, error) {
	token, err := c.handshake()
	if err != nil {
		return "", err
	}
	return c.llpCallWithToken(token, promptText)
}

func (c *Client) llpCallWithToken(token, promptText string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": c.effectiveModel(),
		"messages": []map[string]string{
			{"role": "user", "content": promptText},
		},
	})

	url := strings.TrimRight(c.URL, "/") + "/v1/chat/completions"
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 300 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("LLP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLP %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding LLP response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("LLP returned empty choices")
	}
	return result.Choices[0].Message.Content, nil
}

func (c *Client) handshake() (string, error) {
	socket := c.effectiveSocket()
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socket)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	resp, err := client.Post("http://unix/register", "application/json",
		bytes.NewReader([]byte(`{"agent":"gml"}`)))
	if err != nil {
		return "", fmt.Errorf("LLP handshake via %s: %w (is LLP running?)", socket, err)
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing handshake response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("LLP handshake returned empty token")
	}
	return result.Token, nil
}

func (c *Client) cliCall(model, promptText string) (string, error) {
	switch model {
	case "claude":
		cmd := exec.Command("claude", "-p", "--model", "claude-opus-4-6", "--output-format", "text")
		cmd.Stdin = strings.NewReader(promptText)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("claude: %w\nstderr: %s", err, stderr.String())
		}
		return stdout.String(), nil

	case "gemini":
		gcpProject := os.Getenv("GOOGLE_CLOUD_PROJECT")
		if gcpProject == "" {
			return "", fmt.Errorf("set GOOGLE_CLOUD_PROJECT or use LLP proxy (LLP_URL)")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "npx", "@google/gemini-cli", "-e", "none", "--approval-mode", "default", "-p", "")
		cmd.Env = append(os.Environ(), "GOOGLE_CLOUD_PROJECT="+gcpProject)
		cmd.Stdin = strings.NewReader(promptText)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		fmt.Fprintf(os.Stderr, "  [gemini] exit=%v prompt=%dB response=%dB\n", err, len(promptText), stdout.Len())
		if err != nil {
			for _, line := range strings.Split(stderr.String(), "\n") {
				if line != "" {
					fmt.Fprintf(os.Stderr, "    %s\n", line)
				}
			}
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Fprintln(os.Stderr, "  [gemini] TIMED OUT after 180s")
			}
			return "", fmt.Errorf("gemini: %w", err)
		}
		return stdout.String(), nil

	default:
		return "", fmt.Errorf("unknown model %q (use claude or gemini)", model)
	}
}
