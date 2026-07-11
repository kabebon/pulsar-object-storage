package domain

import (
	"context"
	"testing"
)

func TestNormalizeDomain(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"HTTPS://Foo.Example.COM/Path", "foo.example.com"},
		{"cdn.example.com.", "cdn.example.com"},
		{"  bar.example.com  ", "bar.example.com"},
		{"a.io?q=1", "a.io"},
	}
	for _, c := range cases {
		if got := normalizeDomain(c.in); got != c.want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateDomain(t *testing.T) {
	t.Parallel()
	good := []string{"cdn.example.com", "a.io", "static.files.example.org"}
	for _, d := range good {
		if err := validateDomain(d); err != nil {
			t.Errorf("expected %q valid, got %v", d, err)
		}
	}
	bad := []string{"", "localhost", "no dot", "has space.com", "under_score.com", "слон.рф"}
	for _, d := range bad {
		if err := validateDomain(d); err == nil {
			t.Errorf("expected %q invalid", d)
		}
	}
}

// fakeResolver lets us drive checkTXT/checkCNAME deterministically.
type fakeResolver struct{ txt []string; cname string }

func (f *fakeResolver) LookupTXT(ctx context.Context, host string) ([]string, error) {
	return f.txt, nil
}
func (f *fakeResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	return f.cname, nil
}

func TestCheckTXT(t *testing.T) {
	t.Parallel()
	s := &Service{Deps: Deps{Resolver: &fakeResolver{txt: []string{"pulsar-verify=abc", "v=spf1 -all"}}}}
	if !s.checkTXT(context.Background(), "cdn.example.com") {
		t.Error("expected TXT match")
	}
	s2 := &Service{Deps: Deps{Resolver: &fakeResolver{txt: []string{"v=spf1 -all"}}}}
	if s2.checkTXT(context.Background(), "cdn.example.com") {
		t.Error("unexpected TXT match")
	}
}

func TestCheckCNAME(t *testing.T) {
	t.Parallel()
	s := &Service{Deps: Deps{Resolver: &fakeResolver{cname: "cdn.pulsar.example.com."}, CDNTarget: "cdn.pulsar.example.com"}}
	if !s.checkCNAME(context.Background(), "cdn.example.com") {
		t.Error("expected CNAME match")
	}
	s2 := &Service{Deps: Deps{Resolver: &fakeResolver{cname: "other.example.com"}, CDNTarget: "cdn.pulsar.example.com"}}
	if s2.checkCNAME(context.Background(), "cdn.example.com") {
		t.Error("unexpected CNAME match")
	}
	s3 := &Service{Deps: Deps{Resolver: &fakeResolver{cname: ""}, CDNTarget: ""}}
	if s3.checkCNAME(context.Background(), "cdn.example.com") {
		t.Error("expected no match when CDNTarget empty")
	}
}

func TestCDNURL(t *testing.T) {
	t.Parallel()
	s := &Service{}
	got := s.CDNURL("cdn.example.com/", "/path/file.txt")
	want := "https://cdn.example.com/path/file.txt"
	if got != want {
		t.Errorf("CDNURL = %q, want %q", got, want)
	}
}
