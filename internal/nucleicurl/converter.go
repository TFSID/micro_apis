package nucleicurl

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	MaxTemplateBytes = 1 << 20
	MaxRequests      = 20
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

type Options struct {
	BaseURL   string            `json:"base_url,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
}

type Command struct {
	Index   int      `json:"index"`
	Method  string   `json:"method"`
	URL     string   `json:"url"`
	Args    []string `json:"args"`
	Command string   `json:"command"`
}

type Result struct {
	TemplateID string    `json:"template_id,omitempty"`
	Commands   []Command `json:"commands"`
	Warnings   []string  `json:"warnings"`
}

type template struct {
	ID        string         `yaml:"id"`
	HTTP      []httpRequest  `yaml:"http"`
	Requests  []httpRequest  `yaml:"requests"`
	Variables map[string]any `yaml:"variables"`
	Info      map[string]any `yaml:"info"`
	DNS       any            `yaml:"dns"`
	TCP       any            `yaml:"tcp"`
	SSL       any            `yaml:"ssl"`
	Websocket any            `yaml:"websocket"`
	Headless  any            `yaml:"headless"`
	Code      any            `yaml:"code"`
	File      any            `yaml:"file"`
	Workflow  any            `yaml:"workflows"`
}

type httpRequest struct {
	Method       string            `yaml:"method"`
	Path         []string          `yaml:"path"`
	Raw          []string          `yaml:"raw"`
	Headers      map[string]string `yaml:"headers"`
	Body         string            `yaml:"body"`
	Payloads     map[string]any    `yaml:"payloads"`
	Unsafe       bool              `yaml:"unsafe"`
	Pipeline     bool              `yaml:"pipeline"`
	Race         bool              `yaml:"race"`
	Redirects    bool              `yaml:"redirects"`
	MaxRedirects int               `yaml:"max-redirects"`
	CookieReuse  bool              `yaml:"cookie-reuse"`
}

type parsedRequest struct {
	method       string
	target       string
	headers      map[string]string
	body         string
	redirects    bool
	maxRedirects int
}

// Convert parses a bounded subset of Nuclei HTTP templates and renders, but never
// executes, each concrete HTTP request as a POSIX-shell cURL command.
func Convert(source string, options Options) (Result, error) {
	if strings.TrimSpace(source) == "" {
		return Result{}, errors.New("template is required")
	}
	if len(source) > MaxTemplateBytes {
		return Result{}, fmt.Errorf("template exceeds %d byte limit", MaxTemplateBytes)
	}

	var doc template
	if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
		return Result{}, fmt.Errorf("invalid YAML template: %w", err)
	}
	if doc.DNS != nil || doc.TCP != nil || doc.SSL != nil || doc.Websocket != nil || doc.Headless != nil || doc.Code != nil || doc.File != nil || doc.Workflow != nil {
		return Result{}, errors.New("only HTTP templates are supported")
	}
	requests := doc.HTTP
	if len(doc.Requests) > 0 {
		if len(requests) > 0 {
			return Result{}, errors.New("template cannot define both http and requests")
		}
		requests = doc.Requests
	}
	if len(requests) == 0 {
		return Result{}, errors.New("template must contain at least one http request")
	}
	if len(requests) > MaxRequests {
		return Result{}, fmt.Errorf("template exceeds %d request limit", MaxRequests)
	}

	values := make(map[string]string, len(doc.Variables)+len(options.Variables)+2)
	for key, value := range doc.Variables {
		values[key] = fmt.Sprint(value)
	}
	for key, value := range options.Variables {
		values[key] = value
	}
	if options.BaseURL != "" {
		baseURL, err := normalizeBaseURL(options.BaseURL)
		if err != nil {
			return Result{}, err
		}
		values["BaseURL"] = baseURL
		values["RootURL"] = baseURL
		u, _ := url.Parse(baseURL)
		values["Hostname"] = u.Host
		values["Host"] = u.Host
	}

	result := Result{TemplateID: doc.ID, Commands: make([]Command, 0, MaxRequests)}
	for _, request := range requests {
		if request.Unsafe || request.Pipeline || request.Race || request.CookieReuse {
			return Result{}, errors.New("unsafe, pipeline, race, and cookie-reuse request modes are unsupported")
		}
		if len(request.Payloads) > 0 {
			return Result{}, errors.New("payload generators are unsupported; supply concrete values through variables")
		}
		if len(request.Raw) > 0 {
			for _, raw := range request.Raw {
				parsed, err := parseRawRequest(raw)
				if err != nil {
					return Result{}, err
				}
				parsed.redirects = request.Redirects
				parsed.maxRedirects = request.MaxRedirects
				command, err := buildCommand(parsed, values, len(result.Commands)+1)
				if err != nil {
					return Result{}, err
				}
				result.Commands = append(result.Commands, command)
				if len(result.Commands) > MaxRequests {
					return Result{}, fmt.Errorf("expanded template exceeds %d request limit", MaxRequests)
				}
			}
			continue
		}
		if len(request.Path) == 0 {
			return Result{}, errors.New("HTTP request must define path or raw")
		}
		method := request.Method
		if method == "" {
			method = "GET"
		}
		for _, path := range request.Path {
			parsed := parsedRequest{method: method, target: path, headers: request.Headers, body: request.Body, redirects: request.Redirects, maxRedirects: request.MaxRedirects}
			command, err := buildCommand(parsed, values, len(result.Commands)+1)
			if err != nil {
				return Result{}, err
			}
			result.Commands = append(result.Commands, command)
			if len(result.Commands) > MaxRequests {
				return Result{}, fmt.Errorf("expanded template exceeds %d request limit", MaxRequests)
			}
		}
	}
	if len(result.Commands) == 0 {
		return Result{}, errors.New("template contains no convertible requests")
	}
	result.Warnings = []string{"Commands are generated only and are never executed.", "Matcher and extractor evaluation, workflows, fuzzing, and runtime chaining are not included; each command represents one HTTP request."}
	return result, nil
}

func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", errors.New("base_url must be an absolute http or https URL without credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("base_url cannot contain a query or fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func parseRawRequest(raw string) (parsedRequest, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	sections := strings.SplitN(raw, "\n\n", 2)
	lines := strings.Split(sections[0], "\n")
	if len(lines) == 0 {
		return parsedRequest{}, errors.New("raw HTTP request is empty")
	}
	requestLine := strings.Fields(lines[0])
	if len(requestLine) < 2 {
		return parsedRequest{}, errors.New("raw HTTP request has an invalid request line")
	}
	parsed := parsedRequest{method: requestLine[0], target: requestLine[1], headers: make(map[string]string)}
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			return parsedRequest{}, errors.New("folded raw HTTP headers are unsupported")
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return parsedRequest{}, fmt.Errorf("invalid raw HTTP header %q", line)
		}
		parsed.headers[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	if len(sections) == 2 {
		parsed.body = sections[1]
	}
	return parsed, nil
}

func buildCommand(request parsedRequest, values map[string]string, index int) (Command, error) {
	method := strings.ToUpper(strings.TrimSpace(request.method))
	if method == "" {
		method = "GET"
	}
	for _, r := range method {
		if !((r >= 'A' && r <= 'Z') || r == '-') {
			return Command{}, errors.New("invalid HTTP method")
		}
	}
	target, err := substitute(request.target, values)
	if err != nil {
		return Command{}, err
	}
	baseURL := values["BaseURL"]
	if strings.HasPrefix(target, "/") {
		if baseURL == "" {
			return Command{}, errors.New("absolute base_url is required when request paths are relative")
		}
		target = baseURL + target
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return Command{}, fmt.Errorf("request path %q must resolve to an absolute http or https URL", target)
	}

	args := []string{"--silent", "--show-error", "--request", method, "--url", u.String()}
	headerNames := make([]string, 0, len(request.headers))
	for name := range request.headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		if !validHeaderName(name) {
			return Command{}, fmt.Errorf("invalid HTTP header name %q", name)
		}
		value, err := substitute(request.headers[name], values)
		if err != nil {
			return Command{}, err
		}
		if strings.ContainsAny(value, "\r\n") {
			return Command{}, fmt.Errorf("HTTP header %q contains a line break", name)
		}
		args = append(args, "--header", name+": "+value)
	}
	body, err := substitute(request.body, values)
	if err != nil {
		return Command{}, err
	}
	if body != "" {
		args = append(args, "--data-binary", body)
	}
	if request.redirects {
		args = append(args, "--location")
		if request.maxRedirects > 0 {
			args = append(args, "--max-redirs", fmt.Sprint(request.maxRedirects))
		}
	}
	return Command{Index: index, Method: method, URL: u.String(), Args: args, Command: renderPOSIX(args)}, nil
}

func substitute(input string, values map[string]string) (string, error) {
	return expand(input, values, make(map[string]bool), 0)
}

func expand(input string, values map[string]string, stack map[string]bool, depth int) (string, error) {
	if depth > 16 {
		return "", errors.New("template variable expansion exceeded its recursion limit")
	}
	var missing []string
	var expansionErr error
	output := placeholderPattern.ReplaceAllStringFunc(input, func(match string) string {
		if expansionErr != nil {
			return match
		}
		key := placeholderPattern.FindStringSubmatch(match)[1]
		value, ok := values[key]
		if !ok {
			missing = append(missing, key)
			return match
		}
		if stack[key] {
			expansionErr = fmt.Errorf("cyclic template variable %q", key)
			return match
		}
		stack[key] = true
		resolved, err := expand(value, values, stack, depth+1)
		delete(stack, key)
		if err != nil {
			expansionErr = err
			return match
		}
		return resolved
	})
	if expansionErr != nil {
		return "", expansionErr
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("missing template values: %s", strings.Join(unique(missing), ", "))
	}
	if strings.Contains(output, "{{") || strings.Contains(output, "}}") {
		return "", errors.New("unsupported template expression; use concrete variable values")
	}
	if len(output) > MaxTemplateBytes {
		return "", fmt.Errorf("expanded template value exceeds %d byte limit", MaxTemplateBytes)
	}
	return output, nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			continue
		}
		return false
	}
	return true
}

func unique(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func renderPOSIX(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return "curl " + strings.Join(quoted, " ")
}

// IsValidHTTPURL validates a public-source URL's syntax before any network access.
func IsValidHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, errors.New("template_url must be an absolute http or https URL without credentials")
	}
	if strings.Contains(u.Hostname(), "%") {
		return nil, errors.New("template_url cannot contain an IPv6 zone identifier")
	}
	if u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443")) {
		return nil, errors.New("template_url port must match the scheme default (80 for http or 443 for https)")
	}
	return u, nil
}

// IsPublicIP rejects local, private, reserved, and non-routable destination addresses.
func IsPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		blocked := []string{"2001:db8::/32", "2001:10::/28", "100::/64"}
		for _, cidr := range blocked {
			_, network, _ := net.ParseCIDR(cidr)
			if network.Contains(ip) {
				return false
			}
		}
		return true
	}
	blocked := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
		"192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
	}
	for _, cidr := range blocked {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(v4) {
			return false
		}
	}
	return true
}
