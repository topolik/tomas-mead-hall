package fetch

import (
	"fmt"
	"os"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/creds"
	"github.com/topolik/gml-gmail-agent/internal/gws"
	"github.com/topolik/gml-gmail-agent/internal/sanitize"
)

type Box struct {
	Number int
	Name   string
	Query  string
}

type Email struct {
	ID             string
	From           string
	Subject        string
	Date           string
	Body           sanitize.Result
	InjectionFlags []string
}

type BoxResult struct {
	Box    Box
	Emails []Email
}

var Boxes = []Box{
	{1, "TODO", "is:starred is:unread"},
	{2, "Important Unread", "is:unread is:important label:inbox -is:starred -(label:1-JIRA) -(label:1-Confluence) -(from:info@myvcm.net)"},
	{3, "Mentioning Me", "is:unread {label:(1-Mentioning-me)} -is:starred"},
	{4, "Community", "label:notifications-community label:1-security is:unread"},
	{5, "Not To Be Missed", "is:unread -(is:important) -(label:Notifications-community) -(seclists.org) -(lists.openwall.com) -{label:1-JIRA label:1-Confluence from:info@myvcm.net}"},
}

func FetchAll(cr *creds.Creds, days int) ([]BoxResult, error) {
	return FetchAllWithFilter(cr, fmt.Sprintf("newer_than:%dd", days), "")
}

func FetchAllWithFilter(cr *creds.Creds, timeFilter, exclusions string) ([]BoxResult, error) {
	timeFilter = " " + timeFilter
	if exclusions != "" {
		timeFilter += " " + exclusions
	}

	// Collect IDs seen in boxes 1-5
	seenIDs := map[string]bool{}
	var results []BoxResult

	for _, box := range Boxes {
		query := box.Query + timeFilter
		emails, err := fetchBox(cr, query)
		if err != nil {
			return nil, fmt.Errorf("box %d (%s): %w", box.Number, box.Name, err)
		}
		for _, e := range emails {
			seenIDs[e.ID] = true
		}
		if len(emails) > 0 {
			results = append(results, BoxResult{Box: box, Emails: emails})
		}
	}

	// Box 6: unboxed — all unread minus boxes 1-5
	allQuery := "is:unread" + timeFilter
	allRefs, err := gws.ListMessages(cr, allQuery, 10)
	if err != nil {
		return nil, fmt.Errorf("box 6 (Unboxed): listing: %w", err)
	}
	var unboxedRefs []gws.MessageRef
	for _, ref := range allRefs {
		if !seenIDs[ref.ID] {
			unboxedRefs = append(unboxedRefs, ref)
		}
	}
	if len(unboxedRefs) > 0 {
		emails, err := fetchMessages(cr, unboxedRefs)
		if err != nil {
			return nil, fmt.Errorf("box 6 (Unboxed): fetching: %w", err)
		}
		results = append(results, BoxResult{
			Box:    Box{6, "Unboxed", allQuery},
			Emails: emails,
		})
	}

	return results, nil
}

func fetchBox(cr *creds.Creds, query string) ([]Email, error) {
	refs, err := gws.ListMessages(cr, query, 10)
	if err != nil {
		return nil, err
	}
	return fetchMessages(cr, refs)
}

func fetchMessages(cr *creds.Creds, refs []gws.MessageRef) ([]Email, error) {
	var emails []Email
	for _, ref := range refs {
		msg, err := gws.GetMessageFull(cr, ref.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to fetch message %s: %v\n", ref.ID, err)
			continue
		}
		body, isHTML := msg.ExtractBody()
		result := sanitize.Process(body, isHTML)

		emails = append(emails, Email{
			ID:             msg.ID,
			From:           extractEmail(msg.From()),
			Subject:        msg.Subject(),
			Date:           msg.Header("date"),
			Body:           result,
			InjectionFlags: result.InjectionFlags,
		})
	}
	return emails, nil
}

func extractEmail(from string) string {
	if start := strings.Index(from, "<"); start != -1 {
		if end := strings.Index(from[start:], ">"); end != -1 {
			return from[start+1 : start+end]
		}
	}
	return strings.TrimSpace(from)
}
