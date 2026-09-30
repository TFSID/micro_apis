package nucleicurl

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const MaxFetchedTemplateBytes = MaxTemplateBytes

type IPResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type Fetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, error)
}

type publicFetcher struct {
	client *http.Client
}

// NewPublicFetcher builds a fetcher with DNS pinning, private-network blocking,
// no ambient proxy, bounded response reads, and redirects disabled.
func NewPublicFetcher() Fetcher {
	resolver := net.DefaultResolver
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			return dialPublic(ctx, resolver, network, host, port)
		},
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
		DisableKeepAlives:     true,
	}
	return &publicFetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   6 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (f *publicFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := IsValidHTTPURL(rawURL)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !IsPublicIP(ip) {
		return nil, fmt.Errorf("template_url resolves to a blocked IP address")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid template_url: %w", err)
	}
	request.Header.Set("Accept", "text/yaml, application/yaml, text/plain, */*")
	request.Header.Set("User-Agent", "micro-api-template-converter/1.0")

	response, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch template: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch template: remote server returned HTTP %s", response.Status)
	}
	if response.ContentLength > MaxFetchedTemplateBytes {
		return nil, fmt.Errorf("fetched template exceeds %d byte limit", MaxFetchedTemplateBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxFetchedTemplateBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read template: %w", err)
	}
	if len(body) > MaxFetchedTemplateBytes {
		return nil, fmt.Errorf("fetched template exceeds %d byte limit", MaxFetchedTemplateBytes)
	}
	return body, nil
}

func dialPublic(ctx context.Context, resolver IPResolver, network, host, port string) (net.Conn, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return nil, fmt.Errorf("destination IP is blocked")
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	if strings.EqualFold(host, "localhost") || !strings.Contains(host, ".") {
		return nil, fmt.Errorf("local hostnames are blocked")
	}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("resolve destination host")
	}
	for _, candidate := range ips {
		if !IsPublicIP(candidate.IP) {
			return nil, fmt.Errorf("destination host resolves to a blocked IP address")
		}
	}
	var lastErr error
	for _, candidate := range ips {
		address := net.JoinHostPort(candidate.IP.String(), port)
		conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// OfficialTemplateURL builds a raw URL for a path within the official template
// repository. Paths are restricted to templates/ and cannot escape that tree.
func OfficialTemplateURL(templatePath string) (string, error) {
	clean := strings.TrimSpace(templatePath)
	if clean == "" || strings.Contains(clean, "\\") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("template_path must be relative to templates/")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("template_path contains an invalid path segment")
		}
	}
	if !strings.HasSuffix(clean, ".yaml") && !strings.HasSuffix(clean, ".yml") {
		return "", fmt.Errorf("template_path must end in .yaml or .yml")
	}
	u := &url.URL{Scheme: "https", Host: "raw.githubusercontent.com", Path: "/projectdiscovery/nuclei-templates/main/templates/" + clean}
	return u.String(), nil
}

// PathFromTemplateID maps a conventional template filename to the official
// repository's available template path without guessing a category directory.
// Callers should use template_path when repository files are nested by category.
func PathFromTemplateID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\:`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("template_id must be a simple template filename or ID")
	}
	if !strings.HasSuffix(id, ".yaml") && !strings.HasSuffix(id, ".yml") {
		id += ".yaml"
	}
	return OfficialTemplateURL(id)
}
