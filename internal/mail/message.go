package mail

import (
	"fmt"
	"strings"
	"text/template"
	"time"
)

type Message struct {
	Account   string
	UID       uint32
	From      string
	Subject   string
	Date      time.Time
	Preview   string
	MessageID string
	TitleTmpl string
	TextTmpl  string
}

func (m Message) Title() string {
	if m.TitleTmpl != "" {
		return renderTmpl(m.TitleTmpl, m)
	}
	subject := strings.TrimSpace(m.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	return subject
}

func (m Message) Text() string {
	if m.TextTmpl != "" {
		return renderTmpl(m.TextTmpl, m)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📬 New mail on %s\n", m.Account)
	fmt.Fprintf(&b, "From: %s\n", orDash(m.From))
	fmt.Fprintf(&b, "Subject: %s\n", orDash(m.Subject))
	if !m.Date.IsZero() {
		fmt.Fprintf(&b, "Date: %s\n", m.Date.Local().Format("2006-01-02 15:04:05"))
	}
	if p := strings.TrimSpace(m.Preview); p != "" {
		fmt.Fprintf(&b, "\n%s", p)
	}
	return strings.TrimRight(b.String(), "\n")
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func renderTmpl(tmpl string, data any) string {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return tmpl
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return tmpl
	}
	return buf.String()
}
