package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"micro-api/internal/nucleicurl"
)

type emptyInput struct{}

type healthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"Service health status"`
	}
}

type greetingInput struct {
	Name string `path:"name" minLength:"1" maxLength:"30" doc:"Name to greet"`
}

type greetingOutput struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

type convertInput struct {
	Body struct {
		TemplateYAML string            `json:"template_yaml,omitempty" doc:"Nuclei YAML template content"`
		TemplateURL  string            `json:"template_url,omitempty" doc:"Public HTTP(S) URL that serves a Nuclei YAML template"`
		TemplatePath string            `json:"template_path,omitempty" doc:"Relative YAML path under the official nuclei-templates/templates directory"`
		TemplateID   string            `json:"template_id,omitempty" doc:"Simple template filename in the root of the official templates directory"`
		BaseURL      string            `json:"base_url,omitempty" doc:"Absolute target base URL used for relative request paths"`
		Variables    map[string]string `json:"variables,omitempty" doc:"Concrete values for template placeholders"`
	}
}

type convertOutput struct {
	Body nucleicurl.Result
}

type convertURLInput struct {
	Body struct {
		URL string `json:"url" minLength:"1" maxLength:"2048" doc:"Public URL of the Nuclei YAML template"`
	}
}

type externalFetchInput struct {
	Body struct {
		URL string `json:"url" minLength:"1" maxLength:"2048" doc:"Public HTTP(S) URL to fetch with an outbound GET request"`
	}
}

type externalFetchOutput struct {
	Body struct {
		URL             string `json:"url" doc:"Fetched URL"`
		Body            string `json:"body" doc:"Response body as UTF-8 text or base64 when binary"`
		IsBase64Encoded bool   `json:"is_base64_encoded" doc:"Whether body is base64 encoded"`
	}
}

// NewHandler builds the HTTP handler used by both local development and Lambda.
func NewHandler() http.Handler {
	return NewHandlerWithFetcher(nucleicurl.NewPublicFetcher())
}

// NewHandlerWithFetcher creates the service handler using a supplied template fetcher.
func NewHandlerWithFetcher(fetcher nucleicurl.Fetcher) http.Handler {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, nucleicurl.MaxTemplateBytes+(256<<10))
			next.ServeHTTP(w, r)
		})
	})
	api := humachi.New(router, huma.DefaultConfig("Micro API", "0.1.0"))

	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Tags:        []string{"system"},
	}, func(_ context.Context, _ *emptyInput) (*healthOutput, error) {
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-greeting",
		Method:      http.MethodGet,
		Path:        "/greeting/{name}",
		Summary:     "Greet someone",
		Tags:        []string{"examples"},
	}, func(_ context.Context, input *greetingInput) (*greetingOutput, error) {
		out := &greetingOutput{}
		out.Body.Message = "Hello, " + input.Name + "!"
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "convert-nuclei-template",
		Method:      http.MethodPost,
		Path:        "/convert",
		Summary:     "Convert a Nuclei HTTP template into cURL commands",
		Description: "Parses concrete HTTP requests and returns commands without executing them. Exactly one template source must be provided.",
		Tags:        []string{"converter"},
	}, func(ctx context.Context, input *convertInput) (*convertOutput, error) {
		body := input.Body
		sourceCount := 0
		for _, present := range []bool{body.TemplateYAML != "", body.TemplateURL != "", body.TemplatePath != "", body.TemplateID != ""} {
			if present {
				sourceCount++
			}
		}
		if sourceCount != 1 {
			return nil, huma.Error400BadRequest("provide exactly one of template_yaml, template_url, template_path, or template_id")
		}

		templateYAML := body.TemplateYAML
		if body.TemplateURL != "" || body.TemplatePath != "" || body.TemplateID != "" {
			urlToFetch := body.TemplateURL
			if body.TemplatePath != "" {
				var err error
				urlToFetch, err = nucleicurl.OfficialTemplateURL(body.TemplatePath)
				if err != nil {
					return nil, huma.Error400BadRequest("invalid template_path", err)
				}
			}
			if body.TemplateID != "" {
				var err error
				urlToFetch, err = nucleicurl.PathFromTemplateID(body.TemplateID)
				if err != nil {
					return nil, huma.Error400BadRequest("invalid template_id", err)
				}
			}
			if _, err := nucleicurl.IsValidHTTPURL(urlToFetch); err != nil {
				return nil, huma.Error400BadRequest("invalid template_url", err)
			}
			fetched, err := fetcher.Fetch(ctx, urlToFetch)
			if err != nil {
				return nil, huma.NewError(http.StatusBadGateway, fmt.Sprintf("could not fetch template: %v", err))
			}
			templateYAML = string(fetched)
		}

		result, err := nucleicurl.Convert(templateYAML, nucleicurl.Options{
			BaseURL:   body.BaseURL,
			Variables: body.Variables,
		})
		if err != nil {
			return nil, huma.Error400BadRequest("template cannot be converted", err)
		}
		return &convertOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "convert-nuclei-template-url",
		Method:      http.MethodPost,
		Path:        "/convert/url",
		Summary:     "Convert a Nuclei template from its URL",
		Description: "Fetches one public Nuclei YAML URL and returns cURL commands. Replace the TARGET placeholder with the target host before running a command.",
		Tags:        []string{"converter"},
	}, func(ctx context.Context, input *convertURLInput) (*convertOutput, error) {
		if _, err := nucleicurl.IsValidHTTPURL(input.Body.URL); err != nil {
			return nil, huma.Error400BadRequest("invalid url", err)
		}
		templateYAML, err := fetcher.Fetch(ctx, input.Body.URL)
		if err != nil {
			return nil, huma.NewError(http.StatusBadGateway, fmt.Sprintf("could not fetch template: %v", err))
		}
		result, err := nucleicurl.Convert(string(templateYAML), nucleicurl.Options{BaseURL: "https://TARGET"})
		if err != nil {
			return nil, huma.Error400BadRequest("template cannot be converted", err)
		}
		result.Warnings = append(result.Warnings, "Replace https://TARGET with the actual target base URL before running the generated commands.")
		return &convertOutput{Body: result}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "fetch-public-url",
		Method:      http.MethodPost,
		Path:        "/external/fetch",
		Summary:     "Fetch a public external URL",
		Description: "Makes an outbound HTTP GET request to a public internet host and returns a bounded response body. Private, local, and reserved network addresses are blocked; redirects are not followed.",
		Tags:        []string{"external"},
	}, func(ctx context.Context, input *externalFetchInput) (*externalFetchOutput, error) {
		if _, err := nucleicurl.IsValidHTTPURL(input.Body.URL); err != nil {
			return nil, huma.Error400BadRequest("invalid url", err)
		}
		body, err := fetcher.Fetch(ctx, input.Body.URL)
		if err != nil {
			return nil, huma.NewError(http.StatusBadGateway, fmt.Sprintf("could not fetch URL: %v", err))
		}
		out := &externalFetchOutput{}
		out.Body.URL = input.Body.URL
		if utf8.Valid(body) {
			out.Body.Body = string(body)
		} else {
			out.Body.Body = base64.StdEncoding.EncodeToString(body)
			out.Body.IsBase64Encoded = true
		}
		return out, nil
	})

	return router
}
