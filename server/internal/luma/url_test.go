package luma

import "testing"

func TestNormalizeEventURL(t *testing.T) {
	ok := map[string]string{
		"https://lu.ma/founder-stories":          "https://lu.ma/founder-stories",
		" https://www.LU.MA/abc123xy/ ":          "https://www.lu.ma/abc123xy",
		"https://luma.com/Founder-Stories?x=1#y": "https://luma.com/Founder-Stories",
		"https://lu.ma/event/evt-AbCd1234":       "https://lu.ma/event/evt-AbCd1234",
	}
	for in, want := range ok {
		got, err := NormalizeEventURL(in)
		if err != nil || got != want {
			t.Errorf("%q → %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "lu.ma/x1", "http://lu.ma/abc", "https://lu.ma", "https://lu.ma/create", "https://lu.ma/Create/", "https://lu.ma/calendar",
		"https://lu.ma/discover", "https://lu.ma/u/someone", "https://lu.ma/event/manage", "https://lu.ma/event/evt-x", "https://notlu.ma/abc",
		"https://lu.ma@evil.example/abc", "https://lu.ma/%2e%2e", "https://lu.ma/-abc", "https://lu.ma/a", "ftp://lu.ma/abc"} {
		if got, err := NormalizeEventURL(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
}
