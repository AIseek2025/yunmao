package authjwt

import (
	"errors"
	"testing"
)

func TestEnsureHS256Allowed_AlwaysRemoved(t *testing.T) {
	cases := []string{"", "true", "1", "yes", "maybe", "false"}
	for _, v := range cases {
		v := v
		t.Run(v, func(t *testing.T) {
			t.Setenv("YUNMAO_ALLOW_HS256", v)
			if err := EnsureHS256Allowed(); !errors.Is(err, ErrHS256Removed) {
				t.Fatalf("expected ErrHS256Removed (regardless of opt-in env), got %v", err)
			}
		})
	}
}

func TestErrHS256DisabledAliasesRemoved(t *testing.T) {
	if !errors.Is(ErrHS256Disabled, ErrHS256Removed) {
		t.Fatalf("ErrHS256Disabled must alias ErrHS256Removed for backwards compat (got separate sentinels)")
	}
}

func TestNewHSKeyProviderReturnsRemovedError(t *testing.T) {
	if _, err := NewHSKeyProvider("k", []byte("test-shared-secret-1234567890")); !errors.Is(err, ErrHS256Removed) {
		t.Fatalf("expected NewHSKeyProvider to return ErrHS256Removed, got %v", err)
	}
}

func TestNewSignerLegacyEntryReturnsRemovedError(t *testing.T) {
	if _, err := NewSigner([]byte("test-shared-secret-1234567890"), "yunmao.test"); !errors.Is(err, ErrHS256Removed) {
		t.Fatalf("expected NewSigner([]byte) to return ErrHS256Removed, got %v", err)
	}
}

func TestNewVerifierLegacyEntryReturnsRemovedError(t *testing.T) {
	if _, err := NewVerifier([]byte("test-shared-secret-1234567890")); !errors.Is(err, ErrHS256Removed) {
		t.Fatalf("expected NewVerifier([]byte) to return ErrHS256Removed, got %v", err)
	}
}
