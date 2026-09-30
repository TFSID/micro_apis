package lambdahttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"
)

// Handler adapts API Gateway HTTP API (payload format 2.0) events to net/http.
type Handler struct {
	httpHandler http.Handler
}

func New(handler http.Handler) *Handler {
	return &Handler{httpHandler: handler}
}

func (h *Handler) Handle(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	body := []byte(event.Body)
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(event.Body)
		if err != nil {
			return events.APIGatewayV2HTTPResponse{
				StatusCode: http.StatusBadRequest,
				Headers:    map[string]string{"Content-Type": "text/plain; charset=utf-8"},
				Body:       "invalid base64 request body",
			}, nil
		}
		body = decoded
	}

	method := event.RequestContext.HTTP.Method
	if method == "" {
		method = http.MethodGet
	}
	path := event.RawPath
	if path == "" {
		path = event.RequestContext.HTTP.Path
	}
	if path == "" {
		path = "/"
	}
	requestURL, err := url.ParseRequestURI(path)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusBadRequest,
			Headers:    map[string]string{"Content-Type": "text/plain; charset=utf-8"},
			Body:       "invalid request path",
		}, nil
	}
	requestURL.Scheme = "https"
	requestURL.Host = "lambda"
	requestURL.RawQuery = event.RawQueryString
	if requestURL.RawQuery == "" && len(event.QueryStringParameters) > 0 {
		query := url.Values{}
		for key, value := range event.QueryStringParameters {
			query.Set(key, value)
		}
		requestURL.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	for key, value := range event.Headers {
		req.Header.Set(key, value)
	}
	if len(event.Cookies) > 0 {
		req.Header.Set("Cookie", strings.Join(event.Cookies, "; "))
	}

	recorder := httptest.NewRecorder()
	h.httpHandler.ServeHTTP(recorder, req)
	result := recorder.Result()
	defer result.Body.Close()

	response := events.APIGatewayV2HTTPResponse{
		StatusCode: result.StatusCode,
		Headers:    make(map[string]string),
	}
	for key, values := range result.Header {
		if strings.EqualFold(key, "Set-Cookie") {
			response.Cookies = append(response.Cookies, values...)
			continue
		}
		response.Headers[key] = strings.Join(values, ", ")
	}
	responseBody := recorder.Body.Bytes()
	if utf8.Valid(responseBody) {
		response.Body = string(responseBody)
	} else {
		response.Body = base64.StdEncoding.EncodeToString(responseBody)
		response.IsBase64Encoded = true
	}
	return response, nil
}
