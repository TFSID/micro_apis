package nucleicurl

import (
	"context"
	"net"
	"strings"
	"testing"
)

type fixedResolver []net.IPAddr

func (r fixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr(r), nil
}

func TestURLAndIPSafetyChecks(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"http://example.com:8080/template.yaml",
		"http://user:pass@example.com/template.yaml",
	} {
		if _, err := IsValidHTTPURL(raw); err == nil {
			t.Errorf("IsValidHTTPURL(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1/template.yaml", "http://169.254.169.254/latest/meta-data"} {
		fetcher := NewPublicFetcher()
		if _, err := fetcher.Fetch(context.Background(), raw); err == nil {
			t.Errorf("Fetch(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	cases := []struct {
		address string
		want    bool
	}{
		{"1.1.1.1", true},
		{"10.0.0.1", false},
		{"100.64.0.1", false},
		{"192.0.2.1", false},
		{"127.0.0.1", false},
		{"169.254.169.254", false},
		{"2606:4700:4700::1111", true},
		{"2001:db8::1", false},
		{"fd00::1", false},
		{"::1", false},
	}
	for _, test := range cases {
		if got := IsPublicIP(net.ParseIP(test.address)); got != test.want {
			t.Errorf("IsPublicIP(%s) = %v, want %v", test.address, got, test.want)
		}
	}
}

func TestDialPublicRejectsPrivateDNSAnswers(t *testing.T) {
	resolver := fixedResolver{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}
	if _, err := dialPublic(context.Background(), resolver, "tcp", "templates.example", "443"); err == nil {
		t.Fatal("expected private DNS answer to be rejected before dialing")
	}
}

func TestOfficialTemplatePathValidation(t *testing.T) {
	got, err := OfficialTemplateURL("http/cves/2026/example.yaml")
	if err != nil || !strings.Contains(got, "/templates/http/cves/2026/example.yaml") {
		t.Fatalf("OfficialTemplateURL() = %q, %v", got, err)
	}
	for _, path := range []string{"../secret.yaml", "/absolute.yaml", `http\\evil.yaml`, "no-extension"} {
		if _, err := OfficialTemplateURL(path); err == nil {
			t.Errorf("OfficialTemplateURL(%q) unexpectedly succeeded", path)
		}
	}
}
