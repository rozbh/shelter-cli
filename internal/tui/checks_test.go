package tui

import "testing"

func TestRttRegex(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"64 bytes from 8.8.8.8: icmp_seq=1 ttl=115 time=12.3 ms", "12.3"},
		{"64 bytes from 1.1.1.1: icmp_seq=1 ttl=59 time<1 ms", "1"},
		{"no match here", ""},
	}
	for _, c := range cases {
		m := rttRe.FindStringSubmatch(c.line)
		got := ""
		if len(m) == 2 {
			got = m[1]
		}
		if got != c.want {
			t.Errorf("rttRe on %q = %q, want %q", c.line, got, c.want)
		}
	}
}
