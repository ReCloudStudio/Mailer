package mail

import (
	"strings"
	"testing"
)

func TestCleanCollapsesBlankLines(t *testing.T) {
	// Mirrors the OTP-mail pain point: HTML layouts emit one newline per
	// block tag, producing walls of blank lines between real content.
	in := "Cloud，您好\r\n\r\n\r\n\r\n请输入这个 6 位代码，以验证您的身份：\r\n\r\n\r\n690831\r\n\r\n该安全码有效时间为 10 分钟。\r\n\r\n\r\n"
	want := "Cloud，您好\n\n请输入这个 6 位代码，以验证您的身份：\n\n690831\n\n该安全码有效时间为 10 分钟。"
	if got := clean(in); got != want {
		t.Errorf("clean() =\n%q\nwant\n%q", got, want)
	}
}

func TestCleanReplacesInvisibleSpaces(t *testing.T) {
	// nbsp/ideographic space become a real space; zero-width chars vanish.
	tests := []struct{ in, want string }{
		{"a\u00a0b", "a b"},
		{"a\u3000b", "a b"},
		{"a\u200bb", "ab"},
		{"a\ufeffb", "ab"},
		{"a\u2007b", "ab"},
	}
	for _, tc := range tests {
		if got := clean(tc.in); got != tc.want {
			t.Errorf("clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCleanCollapsesWhitespaceOnlyLines(t *testing.T) {
	// Lines containing only (converted) spaces must count as blank.
	in := "one\n \u00a0 \n\n \ntwo"
	if got := clean(in); got != "one\n\ntwo" {
		t.Errorf("clean() = %q, want %q", got, "one\n\ntwo")
	}
}

func TestCapRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"shorter untouched", "hello", 10, "hello"},
		{"exact fits", "hello", 5, "hello"},
		{"cuts with ellipsis", "hello world", 8, "hello wo…"},
		{"trims cut space", "hello world", 5, "hello…"},
		{"multibyte runes", "你好世界你好世界", 4, "你好世界…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := capRunes(tc.s, tc.max); got != tc.want {
				t.Errorf("capRunes(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
			}
		})
	}
}

func TestHTMLToTextSkipsTrackingPixels(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		wantNotIn string
	}{
		{"1px", `<p>hi</p><img src="https://x/y.gif" width="1" height="1" alt="">`, "y.gif"},
		{"2px px-suffix", `<p>hi</p><img src="https://x/y.gif" width="2px">`, "y.gif"},
		{"data uri no dims", `<p>hi</p><img src="data:image/gif;base64,R0lGOD">`, "data:image"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := htmlToText(tc.html)
			if !strings.Contains(got, "hi") {
				t.Errorf("text lost body content: %q", got)
			}
			if strings.Contains(got, tc.wantNotIn) {
				t.Errorf("htmlToText(%q) = %q, should not contain %q", tc.html, got, tc.wantNotIn)
			}
		})
	}
	// The kept real image must render as a markdown link.
	if got := htmlToText(`<p>hi</p><img src="https://x/logo.png" width="64" alt="Logo">`); !strings.Contains(got, "[Logo](https://x/logo.png)") {
		t.Errorf("real image not rendered: %q", got)
	}
}

func TestHTMLToTextCollapsesSpacerRows(t *testing.T) {
	// Typical table-based newsletter skeleton: empty cells + spacer imgs.
	html := `<table>
<tr><td height="10"><img src="https://x/spacer.gif" width="600" height="1"></td></tr>
<tr><td>First line</td></tr>
<tr><td height="10">&nbsp;</td></tr>
<tr><td>Second line</td></tr>
</table>`
	got := htmlToText(html)
	if strings.Contains(got, "spacer.gif") {
		t.Errorf("spacer img leaked: %q", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("blank-line run not collapsed: %q", got)
	}
	if !strings.Contains(got, "First line\n\nSecond line") {
		t.Errorf("unexpected layout: %q", got)
	}
}

func TestExtractPreviewPlainTruncates(t *testing.T) {
	body := "abcdefghij"
	msg := "Subject: t\nContent-Type: text/plain; charset=utf-8\n\n" + body
	if got := extractPreview([]byte(msg), 4); got != "abcd…" {
		t.Errorf("extractPreview cap = %q, want %q", got, "abcd…")
	}
	if got := extractPreview([]byte(msg), 0); got != body {
		t.Errorf("default cap changed short text: %q", got)
	}
}

func TestExtractPreviewHTMLOverPlainFallback(t *testing.T) {
	// multipart/alternative: text/plain part wins when present.
	msg := `Subject: t
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="X"

--X
Content-Type: text/plain; charset=utf-8

Plain body
--X
Content-Type: text/html; charset=utf-8

<p>HTML <b>body</b></p><img src="https://x/p.gif" width="1" height="1">
--X--
`
	if got := extractPreview([]byte(msg), 400); got != "Plain body" {
		t.Errorf("extractPreview = %q, want %q", got, "Plain body")
	}

	// HTML-only document: converted to text, spacers dropped.
	msgHTML := `Subject: t
MIME-Version: 1.0
Content-Type: text/html; charset=utf-8

<html><body>

<div>Hello&nbsp;there</div>


<div><img src="https://x/p.gif" width="1" height="1" alt=""></div>
<div>Code 690831</div>

</body></html>`
	got := extractPreview([]byte(msgHTML), 400)
	want := "Hello there\n\nCode 690831"
	if got != want {
		t.Errorf("extractPreview html = %q, want %q", got, want)
	}
}
