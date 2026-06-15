package gws

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Allowlisted gws Gmail API methods. Any new method requires an explicit addition here.
var allowedMethods = map[string]bool{
	"users.getProfile":      true,
	"users.messages.list":   true,
	"users.messages.get":    true,
	"users.messages.modify": true,
	"users.threads.list":    true,
	"users.threads.get":     true,
	"users.labels.list":     true,
	"users.labels.create":   true,
}

func TestNoSendOrDraftAPICalls(t *testing.T) {
	src, err := os.ReadFile("gws.go")
	if err != nil {
		t.Fatalf("reading gws.go: %v", err)
	}

	// Match Run/RunJSON calls that start with "gmail", capture remaining args
	re := regexp.MustCompile(`(?:Run|RunJSON)\([^)]*"gmail"((?:,\s*"[^"]*")*)`)
	argRe := regexp.MustCompile(`"([^"]*)"`)

	for _, match := range re.FindAllStringSubmatch(string(src), -1) {
		var parts []string
		for _, a := range argRe.FindAllStringSubmatch(match[1], -1) {
			if strings.HasPrefix(a[1], "--") {
				break
			}
			parts = append(parts, a[1])
		}
		method := strings.Join(parts, ".")
		if !allowedMethods[method] {
			t.Errorf("forbidden Gmail API method %q — only allowlisted methods are permitted", method)
		}
	}
}

func TestArchiveOnlyRemovesInbox(t *testing.T) {
	src, err := os.ReadFile("gws.go")
	if err != nil {
		t.Fatalf("reading gws.go: %v", err)
	}

	if strings.Contains(string(src), `"TRASH"`) || strings.Contains(string(src), `"SPAM"`) {
		t.Error("gws.go references TRASH or SPAM labels — only INBOX removal is allowed")
	}
	if strings.Contains(string(src), "messages.delete") || strings.Contains(string(src), "messages.trash") {
		t.Error("gws.go contains delete/trash operations")
	}
}

func TestTracingLabelConstraints(t *testing.T) {
	src, err := os.ReadFile("gws.go")
	if err != nil {
		t.Fatalf("reading gws.go: %v", err)
	}

	// addLabelIds must only appear in ArchiveWithLabel, not in Archive or other functions.
	// Split source by function boundaries and check each.
	funcRe := regexp.MustCompile(`(?m)^func (\w+)\(`)
	matches := funcRe.FindAllStringSubmatchIndex(string(src), -1)
	for i, m := range matches {
		funcName := string(src[m[2]:m[3]])
		end := len(src)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := string(src[m[0]:end])
		if funcName != "ArchiveWithLabel" && strings.Contains(body, "addLabelIds") {
			t.Errorf("function %s contains addLabelIds — only ArchiveWithLabel may add labels", funcName)
		}
	}

	// EnsureTracingLabel must enforce the GML/ prefix
	if !strings.Contains(string(src), `"GML/"`) {
		t.Error("EnsureTracingLabel must enforce GML/ prefix for tracing labels")
	}

	// No labels.delete or labels.patch allowed
	if strings.Contains(string(src), "labels.delete") || strings.Contains(string(src), `labels", "delete`) {
		t.Error("gws.go contains labels.delete — only list and create are allowed")
	}
	if strings.Contains(string(src), "labels.patch") || strings.Contains(string(src), `labels", "patch`) {
		t.Error("gws.go contains labels.patch — only list and create are allowed")
	}
}
