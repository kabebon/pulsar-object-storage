package service

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		password string
	}{
		{"short but valid", "12345678"},
		{"with symbols", `P@ss"word!9#`},
		{"long under bcrypt cap", strings.Repeat("a", 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := HashPassword(tc.password)
			if err != nil {
				t.Fatalf("HashPassword: %v", err)
			}
			if hash == tc.password {
				t.Fatal("hash equals plaintext")
			}
			if !VerifyPassword(hash, tc.password) {
				t.Fatal("VerifyPassword rejected correct password")
			}
			// A genuinely different password must be rejected.
			if VerifyPassword(hash, "totally-different-pw") {
				t.Fatal("VerifyPassword accepted wrong password")
			}
			// Hashes must be salted (unique per call).
			hash2, _ := HashPassword(tc.password)
			if hash == hash2 {
				t.Fatal("bcrypt produced identical hashes; salt not applied")
			}
		})
	}
}

func TestHashPasswordRejectsWeakInput(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",                 // empty
		"short",            // < 8
		"7chars",           // exactly 7
		strings.Repeat("x", 73), // > 72
	}
	for _, pw := range cases {
		if _, err := HashPassword(pw); err == nil {
			t.Errorf("expected error for password len=%d, got nil", len(pw))
		}
	}
}

func TestRandomToken(t *testing.T) {
	t.Parallel()
	tok1, err := RandomToken(32)
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	if len(tok1) < 32 {
		t.Errorf("token too short: %d", len(tok1))
	}
	tok2, _ := RandomToken(32)
	if tok1 == tok2 {
		t.Fatal("two tokens are identical")
	}
	// Default entropy.
	tok3, _ := RandomToken(0)
	if len(tok3) < 32 {
		t.Errorf("default token too short: %d", len(tok3))
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	t.Parallel()
	tok := "abc123"
	h1 := HashToken(tok)
	h2 := HashToken(tok)
	if h1 != h2 {
		t.Fatal("HashToken is not deterministic")
	}
	if h1 == tok {
		t.Fatal("hash equals input")
	}
	if HashToken("different") == h1 {
		t.Fatal("collision between different inputs")
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Foo@Example.COM", "foo@example.com", false},
		{"  user@host.io  ", "user@host.io", false},
		{"", "", true},
		{"noatsign", "", true},
		{"a@b", "", true},            // no dot in domain
		{"a@b.c", "a@b.c", false},
	}
	for _, tc := range cases {
		got, err := NormalizeEmail(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeEmail(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeEmail(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
