package ids

import (
	"strings"
	"testing"
)

func TestNewIDFormat(t *testing.T) {
	id := New(PrefixUser)
	if !strings.HasPrefix(id, "usr_") {
		t.Fatalf("expected usr_ prefix, got %q", id)
	}
	p, _, err := Parse(id)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p != PrefixUser {
		t.Fatalf("got prefix %q", p)
	}
}

func TestParseRejectsBad(t *testing.T) {
	cases := []string{"", "abc", "xx_bad"}
	for _, c := range cases {
		if _, _, err := Parse(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestNewPanicsOnUnknownPrefix(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = New(Prefix("xx"))
}
