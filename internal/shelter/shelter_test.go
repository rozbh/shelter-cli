package shelter

import "testing"

func TestCsrfRegexExtracts(t *testing.T) {
	body := []byte(`<script>const csrf = "abc123XYZ";</script>`)
	m := csrfRe.FindSubmatch(body)
	if len(m) != 2 {
		t.Fatal("csrf regex did not match valid body")
	}
	if got := string(m[1]); got != "abc123XYZ" {
		t.Errorf("got %q, want abc123XYZ", got)
	}
}

func TestCsrfRegexNoMatch(t *testing.T) {
	body := []byte(`<html>no token here</html>`)
	m := csrfRe.FindSubmatch(body)
	if len(m) == 2 {
		t.Error("expected no match on body without csrf")
	}
}

func TestShortTruncates(t *testing.T) {
	if got := short("1234567890123"); got != "1234567890..." {
		t.Errorf("short() = %q", got)
	}
	if got := short("short"); got != "short" {
		t.Errorf("short() should pass through strings <=10 chars, got %q", got)
	}
}
