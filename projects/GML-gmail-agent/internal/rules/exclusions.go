package rules

import (
	"fmt"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

func BuildExclusions(rules []config.Rule) string {
	var parts []string
	for _, r := range rules {
		switch r.Type {
		case "archive_by_sender":
			if r.Params.RequireReply {
				continue
			}
			for _, p := range r.Params.Patterns {
				if !isLiteralSender(p) {
					continue
				}
				if r.Params.Filter != "" {
					parts = append(parts, fmt.Sprintf("-(from:%s %s)", p, r.Params.Filter))
				} else {
					parts = append(parts, fmt.Sprintf("-from:%s", p))
				}
			}
		case "archive_by_label":
			parts = append(parts, fmt.Sprintf("-label:%s", r.Params.Label))
		}
	}
	return strings.Join(parts, " ")
}

func isLiteralSender(s string) bool {
	for _, c := range s {
		if strings.ContainsRune(`^$()+?[]{}\\*|`, c) {
			return false
		}
	}
	return true
}
