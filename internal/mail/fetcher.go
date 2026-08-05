package mail

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"golang.org/x/net/html"

	"github.com/recloud/mailer/internal/config"
)

const maxBodyBytes = 256 * 1024

const maxPreview = 400

type FetchResult struct {
	Messages    []Message
	HighestUID  uint32
	UIDValidity uint32
}

func dialOptions(ctx context.Context) *imapclient.Options {
	dialer := &net.Dialer{}
	if d, ok := ctx.Deadline(); ok {
		dialer.Deadline = d
	}
	opts := &imapclient.Options{
		WordDecoder: &mime.WordDecoder{CharsetReader: gomessage.CharsetReader},
		Dialer:      dialer,
	}
	if os.Getenv("MAILER_DEBUG_IMAP") != "" {
		opts.DebugWriter = os.Stderr
	}
	return opts
}

func identify(client *imapclient.Client, acc config.Account) error {
	if !acc.SendID {
		return nil
	}
	if !client.Caps().Has(imap.CapID) {
		return nil
	}
	_, err := client.ID(&imap.IDData{
		Name:    "mailer",
		Version: "1.0",
		Vendor:  "recloud",
	}).Wait()
	return err
}

type UIDValidityChanged struct {
	Prev, Current uint32
}

func (e *UIDValidityChanged) Error() string {
	return fmt.Sprintf("UIDVALIDITY changed (%d -> %d)", e.Prev, e.Current)
}

func dial(ctx context.Context, acc config.Account) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", acc.Host, acc.Port)
	options := dialOptions(ctx)

	var (
		client *imapclient.Client
		err    error
	)
	if acc.TLS {
		client, err = imapclient.DialTLS(addr, options)
	} else {
		client, err = imapclient.DialStartTLS(addr, options)
	}
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}

	if err := client.Login(acc.Username, acc.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("login: %w", err)
	}

	if err := identify(client, acc); err != nil {
		client.Logout().Wait()
		client.Close()
		return nil, fmt.Errorf("id: %w", err)
	}

	return client, nil
}

func Fetch(ctx context.Context, client *imapclient.Client, acc config.Account, sinceUID uint32, firstRun bool, prevUIDValidity uint32) (*FetchResult, error) {
	selectData, err := client.Select(acc.Mailbox, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("select %s: %w", acc.Mailbox, err)
	}

	if prevUIDValidity != 0 && selectData.UIDValidity != 0 && selectData.UIDValidity != prevUIDValidity {
		return nil, &UIDValidityChanged{Prev: prevUIDValidity, Current: selectData.UIDValidity}
	}

	result := &FetchResult{
		HighestUID:  sinceUID,
		UIDValidity: selectData.UIDValidity,
	}

	start := sinceUID + 1
	if firstRun && !acc.NotifyExisting {
		if selectData.UIDNext > 0 {
			result.HighestUID = uint32(selectData.UIDNext) - 1
		}
		return result, nil
	}

	uidSet := imap.UIDSet{{Start: imap.UID(start), Stop: imap.UID(0)}}
	criteria := &imap.SearchCriteria{UID: []imap.UIDSet{uidSet}}

	searchData, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return result, nil
	}

	bodyLimit := imap.SectionPartial{Size: maxBodyBytes}
	fetchOptions := &imap.FetchOptions{
		Envelope:     true,
		UID:          true,
		InternalDate: true,
		BodySection: []*imap.FetchItemBodySection{
			{Peek: true, Partial: &bodyLimit},
		},
	}

	buffers, err := client.Fetch(imap.UIDSetNum(uids...), fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	for _, buf := range buffers {
		uid := uint32(buf.UID)
		if uid <= sinceUID {
			continue
		}
		if uid > result.HighestUID {
			result.HighestUID = uid
		}
		result.Messages = append(result.Messages, buildMessage(acc.Name, buf))
	}

	return result, nil
}

func MarkSeen(ctx context.Context, client *imapclient.Client, acc config.Account, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}

	if _, err := client.Select(acc.Mailbox, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", acc.Mailbox, err)
	}

	set := make(imap.UIDSet, 0, len(uids))
	for _, u := range uids {
		set.AddNum(imap.UID(u))
	}
	storeFlags := &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagSeen},
	}
	if err := client.Store(set, storeFlags, nil).Close(); err != nil {
		return fmt.Errorf("store \\Seen: %w", err)
	}
	return nil
}

func buildMessage(accountName string, buf *imapclient.FetchMessageBuffer) Message {
	m := Message{
		Account: accountName,
		UID:     uint32(buf.UID),
	}

	if env := buf.Envelope; env != nil {
		m.Subject = env.Subject
		m.Date = env.Date
		m.From = formatAddresses(env.From)
		m.MessageID = env.MessageID
	}
	if m.Date.IsZero() {
		m.Date = time.Now()
	}

	for _, bs := range buf.BodySection {
		if len(bs.Bytes) == 0 {
			continue
		}
		if preview := extractPreview(bs.Bytes); preview != "" {
			m.Preview = preview
			break
		}
	}

	return m
}

func formatAddresses(addrs []imap.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		addr := a.Addr()
		name := strings.TrimSpace(a.Name)
		switch {
		case name != "" && addr != "":
			parts = append(parts, fmt.Sprintf("%s <%s>", name, addr))
		case addr != "":
			parts = append(parts, addr)
		case name != "":
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}

func extractPreview(raw []byte) string {
	mr, err := mail.CreateReader(strings.NewReader(string(raw)))
	if err != nil {
		return ""
	}
	defer mr.Close()

	var fallback string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			body, _ := io.ReadAll(part.Body)
			text := clean(string(body))
			if text == "" {
				continue
			}
			ct, _, _ := h.ContentType()
			switch ct {
			case "text/plain":
				return truncate(text, maxPreview)
			case "text/html", "text/x-amp-html":
				if fallback == "" {
					fallback = htmlToText(text)
				}
			}
		}
	}
	return truncate(fallback, maxPreview)
}

func htmlToText(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return s
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "head", "noscript", "template", "iframe", "svg", "object":
				return
			case "img":
				src := getAttr(n, "src")
				alt := getAttr(n, "alt")
				if src != "" {
					label := "Image"
					if alt != "" {
						label = alt
					}
					fmt.Fprintf(&b, "[%s](%s)", label, src)
				} else if alt != "" {
					b.WriteString(alt)
				}
				return
			case "a":
				href := getAttr(n, "href")
				text := collectText(n)
				if href != "" && text != "" {
					fmt.Fprintf(&b, "[%s](%s)", text, href)
				} else if text != "" {
					b.WriteString(text)
				} else {
					return
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "br", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6",
				"table", "blockquote", "ul", "ol", "section", "article", "header",
				"footer", "hr":
				b.WriteString("\n")
			}
		}
	}
	walk(doc)
	return clean(b.String())
}

func getAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func collectText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}
