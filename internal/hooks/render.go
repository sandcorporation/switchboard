package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	subjectMax  = 120  // runes
	textMax     = 4000 // runes of mail body text
	otherKeys   = 8    // top-level keys an unknown JSON shows
	otherValMax = 200  // runes of one such value
)

// Notice is a received body turned into mail: a one-line subject and a
// readable text. Neither carries raw JSON.
type Notice struct {
	Subject string
	Text    string
}

// discordBody is the part of a Discord webhook body a person reads.
type discordBody struct {
	Content  string `json:"content"`
	Username string `json:"username"`
	Embeds   []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Author      struct {
			Name string `json:"name"`
		} `json:"author"`
		Fields []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"fields"`
		Footer struct {
			Text string `json:"text"`
		} `json:"footer"`
	} `json:"embeds"`
}

// slackText is a Slack text object, or a plain string where one is allowed.
type slackText struct{ Text string }

func (t *slackText) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &t.Text)
	}
	var o struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return nil // an unexpected shape reads as no text
	}
	t.Text = o.Text
	return nil
}

// slackElement is a block element: rich text sections nest further
// elements; text elements carry text.
type slackElement struct {
	Type     string         `json:"type"`
	Text     slackText      `json:"text"`
	Elements []slackElement `json:"elements"`
}

type slackBlock struct {
	Type     string         `json:"type"`
	Text     slackText      `json:"text"`
	Fields   []slackText    `json:"fields"`
	Elements []slackElement `json:"elements"`
}

type slackBody struct {
	Text        string       `json:"text"`
	Blocks      []slackBlock `json:"blocks"`
	Attachments []struct {
		Pretext  string       `json:"pretext"`
		Title    string       `json:"title"`
		Text     string       `json:"text"`
		Fallback string       `json:"fallback"`
		Blocks   []slackBlock `json:"blocks"`
		Fields   []struct {
			Title string `json:"title"`
			Value string `json:"value"`
		} `json:"fields"`
	} `json:"attachments"`
}

// Render turns a JSON body into a notice: a Discord body (content, embeds),
// a Slack body (text, blocks, attachments), otherwise the first top-level
// keys of an object as lines. redact are strings never to show (the original
// destination); webhook-looking addresses are hidden as well. fallback is
// the subject when the body gives none.
func Render(body []byte, fallback string, redact ...string) (Notice, error) {
	trimmed := bytes.TrimSpace(body)
	if !json.Valid(trimmed) {
		return Notice{}, fmt.Errorf("body is not JSON")
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(trimmed, &keys) // nil for a non-object
	var subject string
	var lines []string
	switch {
	case keys["embeds"] != nil || keys["content"] != nil:
		subject, lines = renderDiscord(trimmed)
	case keys["blocks"] != nil || keys["attachments"] != nil || keys["text"] != nil:
		subject, lines = renderSlack(trimmed)
	default:
		lines = renderOther(trimmed)
	}
	text := clip(strings.TrimSpace(strings.Join(lines, "\n")), textMax)
	if subject == "" {
		subject = firstLine(text)
	}
	subject = clip(firstLine(subject), subjectMax)
	if subject == "" {
		subject = fallback
	}
	n := Notice{Subject: hide(subject, redact), Text: hide(text, redact)}
	return n, nil
}

func renderDiscord(b []byte) (string, []string) {
	var d discordBody
	_ = json.Unmarshal(b, &d)
	var lines []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			lines = append(lines, s)
		}
	}
	add(d.Content)
	subject := firstLine(d.Content)
	for _, e := range d.Embeds {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		add(e.Author.Name)
		add(e.Title)
		add(e.Description)
		for _, f := range e.Fields {
			add(joinField(f.Name, f.Value))
		}
		add(e.Footer.Text)
		if subject == "" {
			subject = firstLine(e.Title)
		}
		if subject == "" {
			subject = firstLine(e.Description)
		}
	}
	if subject == "" {
		subject = d.Username
	}
	return subject, lines
}

func renderSlack(b []byte) (string, []string) {
	var s slackBody
	_ = json.Unmarshal(b, &s)
	var lines []string
	add := func(t string) {
		if t = strings.TrimSpace(t); t != "" {
			lines = append(lines, t)
		}
	}
	add(s.Text)
	subject := firstLine(s.Text)
	header := ""
	blocks := func(bs []slackBlock) {
		for _, bl := range bs {
			if bl.Type == "header" && header == "" {
				header = bl.Text.Text
			}
			add(bl.Text.Text)
			for _, f := range bl.Fields {
				add(f.Text)
			}
			add(elementText(bl.Elements))
		}
	}
	blocks(s.Blocks)
	for _, a := range s.Attachments {
		add(a.Pretext)
		add(a.Title)
		add(a.Text)
		for _, f := range a.Fields {
			add(joinField(f.Title, f.Value))
		}
		blocks(a.Blocks)
		if a.Title == "" && a.Text == "" && len(a.Blocks) == 0 {
			add(a.Fallback)
		}
		if subject == "" {
			subject = firstLine(a.Title)
		}
		if subject == "" {
			subject = firstLine(a.Fallback)
		}
	}
	if header != "" {
		subject = header
	}
	return subject, lines
}

// elementText joins the text of rich text and context elements.
func elementText(es []slackElement) string {
	var sb strings.Builder
	var walk func([]slackElement)
	walk = func(es []slackElement) {
		for _, e := range es {
			sb.WriteString(e.Text.Text)
			walk(e.Elements)
		}
	}
	walk(es)
	return sb.String()
}

// renderOther shows the first top-level keys of an object as "key: value"
// lines in their order in the body; a nested value is cut short.
func renderOther(b []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if d, ok := tok.(json.Delim); err != nil || !ok || d != '{' {
		return []string{clip(compact(b), otherValMax)}
	}
	var lines []string
	total := 0
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			break
		}
		var raw json.RawMessage
		if err = dec.Decode(&raw); err != nil {
			break
		}
		total++
		if len(lines) >= otherKeys {
			continue
		}
		key, _ := kt.(string)
		var s string
		if json.Unmarshal(raw, &s) != nil {
			s = compact(raw)
		}
		lines = append(lines, joinField(key, clip(s, otherValMax)))
	}
	if total > len(lines) {
		lines = append(lines, fmt.Sprintf("(키 %d개 더)", total-len(lines)))
	}
	return lines
}

func compact(b []byte) string {
	var out bytes.Buffer
	if json.Compact(&out, b) != nil {
		return string(b)
	}
	return out.String()
}

func joinField(name, value string) string {
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	switch {
	case name == "":
		return value
	case value == "":
		return name
	}
	return name + ": " + value
}

// firstLine is s's first non-empty line without Markdown heading or
// emphasis marks at its ends.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.Trim(strings.TrimSpace(l), "#*_` ")
		if l != "" {
			return l
		}
	}
	return ""
}

// clip cuts s to max runes and drops control characters but newlines.
func clip(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || !unicode.IsControl(r) {
			return r
		}
		if r == '\t' {
			return ' '
		}
		return -1
	}, s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// webhookAddr matches addresses that look like a webhook's: a body that
// carries one (its own destination, another tool's) must not put it in mail.
var webhookAddr = regexp.MustCompile(`(?i)https?://[^\s"'<>()]*(?:/api/webhooks/|hooks\.slack\.com/|/hooks?/)[^\s"'<>()]*`)

const hidden = "(웹훅 주소 가림)"

func hide(s string, redact []string) string {
	for _, r := range redact {
		if r != "" {
			s = strings.ReplaceAll(s, r, hidden)
		}
	}
	return webhookAddr.ReplaceAllString(s, hidden)
}
