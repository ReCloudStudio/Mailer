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
