package notify

import "testing"

func TestReadCallbackRoundTrip(t *testing.T) {
	cases := []struct {
		account string
		uid     uint32
	}{
		{"inbox", 42},
		{"a:b:c", 1},
		{"primary", 123456789},
	}
	for _, c := range cases {
		data := readCallbackData(c.account, c.uid)
		account, uid, ok := parseReadCallback(data)
		if !ok {
			t.Errorf("parse %q failed", data)
			continue
		}
		if account != c.account || uid != c.uid {
			t.Errorf("round trip %q: got %q:%d, want %q:%d", data, account, uid, c.account, c.uid)
		}
	}
}

func TestParseReadCallbackInvalid(t *testing.T) {
	for _, data := range []string{"", "read", "read:", "read:acct", "read:acct:xyz", "seen:acct:1", "read::1"} {
		if _, _, ok := parseReadCallback(data); ok {
			t.Errorf("parse %q should fail", data)
		}
	}
}

func TestCapText(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"fits untouched", "hello", 4096, "hello"},
		{"exact fit", "hello", 5, "hello"},
		{"cuts one over", "hello", 4, "hel…"},
		{"n=1 empty", "hello", 1, ""},
		{"n=0 empty", "hello", 0, ""},
		{"multibyte", "你好世界你好", 4, "你好世…"},
		{"trims trailing space before ellipsis", "hello world", 6, "hello…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := capText(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("capText(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
			if r := []rune(got); tc.n > 1 && len(r) > tc.n {
				t.Errorf("capText(%q, %d) length %d exceeds limit", tc.s, tc.n, len(r))
			}
		})
	}
}
