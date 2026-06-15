package gmail

import (
	"fmt"
	"strings"
	"unicode"
)

var knownOperators = map[string]bool{
	"from":        true,
	"to":          true,
	"cc":          true,
	"bcc":         true,
	"deliveredto": true,
	"list":        true,
	"subject":     true,
	"label":       true,
	"category":    true,
	"has":         true,
	"is":          true,
	"in":          true,
	"filename":    true,
	"older_than":  true,
	"newer_than":  true,
	"after":       true,
	"before":      true,
	"size":        true,
	"larger":      true,
	"smaller":     true,
	"rfc822msgid": true,
	"AROUND":      true,
}

func OperatorsReference() string {
	return `Gmail search operators (use ONLY these in filter and gmail_search fields):

Content: subject:<word>, "exact phrase", bare word (searches subject+body), AROUND <N>, +word
People: from:, to:, cc:, bcc:, deliveredto:, list:
Labels: label:<name>, category:<name>, has:userlabels, has:nouserlabels
Status: is:starred, is:unread, is:read, is:important, is:muted, in:inbox, in:anywhere, in:snoozed
Attachments: has:attachment, has:drive, has:document, has:youtube, filename:<ext>
Date: after:YYYY/MM/DD, before:YYYY/MM/DD, older_than:<N>d (or m/y), newer_than:<N>d (or m/y)
Size: size:<bytes>, larger:<size>, smaller:<size> (e.g. larger:10M)
Combinators: OR, { } (OR group), - (NOT), ( ) (grouping), AND (implicit between terms)

Examples:
  subject:"SYSTEM ALERT"                → emails with exact phrase in subject
  "false positive" -"true positive"     → body contains "false positive" but not "true positive"
  subject:Confluence older_than:7d      → Confluence in subject, older than 7 days
  has:attachment filename:pdf            → has PDF attachment
  {from:alice@x.com from:bob@y.com}    → from alice OR bob`
}

func ValidateQuery(q string) error {
	if q == "" {
		return nil
	}
	tokens := tokenize(q)
	for _, tok := range tokens {
		// A quoted phrase is a literal body/subject search — a colon inside it
		// (e.g. "principal_email: x@y.com", "Re: foo") is content, not an
		// operator. Only `operator:value` tokens (which never start with a quote)
		// are checked.
		if strings.HasPrefix(strings.TrimLeft(tok, "-"), `"`) {
			continue
		}
		if idx := strings.Index(tok, ":"); idx > 0 {
			op := tok[:idx]
			op = strings.TrimLeft(op, "-")
			if op == "" {
				continue
			}
			if !knownOperators[op] {
				return fmt.Errorf("unknown Gmail search operator %q in query %q", op, q)
			}
		}
	}
	return nil
}

func tokenize(q string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for _, r := range q {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case inQuote:
			current.WriteRune(r)
		case r == '{' || r == '}' || r == '(' || r == ')':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		case unicode.IsSpace(r):
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
