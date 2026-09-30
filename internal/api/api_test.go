package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testFetcher struct {
	content []byte
	err     error
	url     string
}

func (f *testFetcher) Fetch(_ context.Context, rawURL string) ([]byte, error) {
	f.url = rawURL
	return f.content, f.err
}

func TestHealthEndpoint(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %s, want status ok", response.Body.String())
	}
}

func TestGreetingEndpoint(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/greeting/Ada", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"message":"Hello, Ada!"`) {
		t.Fatalf("body = %s, want greeting for Ada", response.Body.String())
	}
}

func TestOpenAPIDocument(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "get-greeting") || !strings.Contains(response.Body.String(), "convert-nuclei-template") || !strings.Contains(response.Body.String(), "convert-nuclei-template-url") {
		t.Fatalf("OpenAPI document does not include expected operations")
	}
}

func TestConvertEndpoint(t *testing.T) {
	handler := NewHandlerWithFetcher(&testFetcher{content: []byte("id: api-test\nhttp:\n  - path: ['{{BaseURL}}/status']\n")})
	request := httptest.NewRequest(http.MethodPost, "/convert", strings.NewReader(`{"template_url":"https://templates.example/test.yaml","base_url":"https://target.example","variables":{}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "curl") || !strings.Contains(response.Body.String(), "https://target.example/status") {
		t.Fatalf("unexpected conversion response: %s", response.Body.String())
	}
}

func TestConvertEndpointRequiresExactlyOneSource(t *testing.T) {
	handler := NewHandlerWithFetcher(&testFetcher{})
	request := httptest.NewRequest(http.MethodPost, "/convert", strings.NewReader(`{"template_yaml":"id: demo"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestConvertURLOnlyEndpoint(t *testing.T) {
	const templateURL = "https://raw.githubusercontent.com/projectdiscovery/nuclei-templates/refs/heads/main/http/cves/2026/CVE-2026-1340.yaml"
	fetcher := &testFetcher{content: []byte("id: cve-2026-1340\nhttp:\n  - method: GET\n    path:\n      - '{{BaseURL}}/login'\n")}
	handler := NewHandlerWithFetcher(fetcher)
	request := httptest.NewRequest(http.MethodPost, "/convert/url", strings.NewReader(`{"url":"`+templateURL+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if fetcher.url != templateURL {
		t.Fatalf("fetch URL = %q, want %q", fetcher.url, templateURL)
	}
	if !strings.Contains(response.Body.String(), "https://TARGET/login") || !strings.Contains(response.Body.String(), "Replace https://TARGET") {
		t.Fatalf("response did not include command and target placeholder warning: %s", response.Body.String())
	}
}

func TestConvertURLOnlyEndpointRejectsBadURL(t *testing.T) {
	fetcher := &testFetcher{}
	handler := NewHandlerWithFetcher(fetcher)
	request := httptest.NewRequest(http.MethodPost, "/convert/url", strings.NewReader(`{"url":"file:///etc/passwd"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if fetcher.url != "" {
		t.Fatalf("invalid URL must not be fetched; got %q", fetcher.url)
	}
}
