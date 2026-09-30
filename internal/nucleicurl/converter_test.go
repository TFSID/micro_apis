package nucleicurl

import (
	"strings"
	"testing"
)

func TestConvertHTTPRequestsAndVariables(t *testing.T) {
	source := `id: demo
info:
  name: Demo
variables:
  version: v1
http:
  - method: POST
    path:
      - "{{BaseURL}}/api/{{version}}"
      - "{{BaseURL}}/health"
    headers:
      X-Name: "{{name}}"
    body: "hello {{name}}"
`
	result, err := Convert(source, Options{
		BaseURL:   "https://example.com/",
		Variables: map[string]string{"name": "Ada's test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TemplateID != "demo" || len(result.Commands) != 2 {
		t.Fatalf("unexpected conversion result: %#v", result)
	}
	first := result.Commands[0]
	if first.URL != "https://example.com/api/v1" || first.Method != "POST" {
		t.Fatalf("unexpected request: %#v", first)
	}
	if !strings.Contains(first.Command, `Ada'"'"'s test`) {
		t.Fatalf("command did not safely quote shell input: %s", first.Command)
	}
	if !strings.Contains(first.Command, "--data-binary") {
		t.Fatalf("command omitted body: %s", first.Command)
	}
}

func TestConvertRawRequest(t *testing.T) {
	source := `id: raw-demo
http:
  - raw:
      - |-
        PUT {{BaseURL}}/api HTTP/1.1
        Content-Type: application/json

        {"value":"{{value}}"}
`
	result, err := Convert(source, Options{BaseURL: "https://example.com", Variables: map[string]string{"value": "ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Commands) != 1 || result.Commands[0].Method != "PUT" || result.Commands[0].URL != "https://example.com/api" {
		t.Fatalf("unexpected raw request conversion: %#v", result)
	}
	if !strings.Contains(result.Commands[0].Command, `{"value":"ok"}`) {
		t.Fatalf("raw body was not substituted: %s", result.Commands[0].Command)
	}
}

func TestConvertRejectsMissingValuesAndUnsupportedPayloads(t *testing.T) {
	_, err := Convert("id: demo\nhttp:\n  - path: ['{{BaseURL}}/{{missing}}']\n", Options{BaseURL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-variable error, got %v", err)
	}
	_, err = Convert("id: demo\nhttp:\n  - path: ['{{BaseURL}}']\n    payloads:\n      id: ['1', '2']\n", Options{BaseURL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "payload generators are unsupported") {
		t.Fatalf("expected unsupported-payload error, got %v", err)
	}
}

func TestConvertRejectsMalformedAndOversizedTemplates(t *testing.T) {
	if _, err := Convert("http: [", Options{}); err == nil {
		t.Fatal("expected YAML parse error")
	}
	if _, err := Convert(strings.Repeat("x", MaxTemplateBytes+1), Options{}); err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestConvertRejectsUnsupportedProtocolAndCycles(t *testing.T) {
	if _, err := Convert("id: mixed\nhttp:\n  - path: ['{{BaseURL}}']\ndns:\n  - name: example.com\n", Options{BaseURL: "https://example.com"}); err == nil {
		t.Fatal("expected mixed protocol template to be rejected")
	}
	cycle := "id: cycle\nvariables:\n  first: '{{second}}'\n  second: '{{first}}'\nhttp:\n  - path: ['{{BaseURL}}/{{first}}']\n"
	if _, err := Convert(cycle, Options{BaseURL: "https://example.com"}); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected variable-cycle error, got %v", err)
	}
}

func TestConvertRedirectsAndRejectsHeaderInjection(t *testing.T) {
	source := "id: redirect\nhttp:\n  - path: ['{{BaseURL}}']\n    redirects: true\n    max-redirects: 3\n"
	result, err := Convert(source, Options{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Commands[0].Command, "--location") || !strings.Contains(result.Commands[0].Command, "--max-redirs") {
		t.Fatalf("redirect flags were not rendered: %s", result.Commands[0].Command)
	}
	badHeader := "id: header\nhttp:\n  - path: ['{{BaseURL}}']\n    headers:\n      'X-Bad\\nName': value\n"
	if _, err := Convert(badHeader, Options{BaseURL: "https://example.com"}); err == nil {
		t.Fatal("expected invalid header to be rejected")
	}
}

func TestShellQuoting(t *testing.T) {
	got := renderPOSIX([]string{"--data-binary", "a'b"})
	want := `curl '--data-binary' 'a'"'"'b'`
	if got != want {
		t.Fatalf("renderPOSIX() = %q, want %q", got, want)
	}
}
