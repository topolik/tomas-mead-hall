package sanitize

import (
	"regexp"
	"strings"
	"unicode"

	"jaytaylor.com/html2text"
)

const (
	datamarker = ""
	maxBodyLen = 15000
)

type Result struct {
	Text           string
	InjectionFlags []string
}

func Process(body string, isHTML bool) Result {
	text := body
	if isHTML {
		text = htmlToText(body)
	}
	text = decodeObfuscation(text)
	text = removeInvisibleChars(text)
	text = truncate(text, maxBodyLen)
	flags := detectInjection(text)
	text = Datamark(text)
	return Result{Text: text, InjectionFlags: flags}
}

func htmlToText(s string) string {
	text, err := html2text.FromString(s, html2text.Options{
		PrettyTables: false,
		OmitLinks:    false,
	})
	if err != nil {
		return stripHTMLFallback(s)
	}
	return text
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func stripHTMLFallback(s string) string {
	return htmlTagRe.ReplaceAllString(s, "")
}

var base64LineRe = regexp.MustCompile(`(?m)^[A-Za-z0-9+/=]{100,}$`)

func decodeObfuscation(s string) string {
	return base64LineRe.ReplaceAllString(s, "")
}

func removeInvisibleChars(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == 0xFEFF: // BOM
			return -1
		case r >= 0x200B && r <= 0x200F: // zero-width chars, LTR/RTL marks
			return -1
		case r >= 0x202A && r <= 0x202E: // direction overrides
			return -1
		case r >= 0x2060 && r <= 0x2064: // invisible operators
			return -1
		case r == 0xE000: // our datamarker
			return -1
		case unicode.Is(unicode.Co, r): // other private use area chars
			return -1
		default:
			return r
		}
	}, s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// Datamark inserts U+E000 between every word (Microsoft Spotlighting defense).
func Datamark(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, datamarker)
}

var injectionPatterns = []struct {
	pattern *regexp.Regexp
	name    string
}{
	{regexp.MustCompile(`(?i)ignore\s+(all\s+)?previous\s+instructions?`), "ignore_previous"},
	{regexp.MustCompile(`(?i)disregard\s+(all\s+)?(previous\s+|above\s+)?instructions?`), "disregard_instructions"},
	{regexp.MustCompile(`(?i)system\s*prompt`), "system_prompt_ref"},
	{regexp.MustCompile(`(?i)you\s+are\s+now\s+a`), "role_override"},
	{regexp.MustCompile(`(?i)new\s+instructions?:\s`), "new_instructions"},
	{regexp.MustCompile(`(?i)output\s+.*\{.*compromised`), "compromise_attempt"},
}

func detectInjection(s string) []string {
	var flags []string
	for _, p := range injectionPatterns {
		if p.pattern.MatchString(s) {
			flags = append(flags, p.name)
		}
	}
	return flags
}
