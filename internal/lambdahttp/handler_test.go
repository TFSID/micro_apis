package lambdahttp

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandleProxiesAPIGatewayV2Request(t *testing.T) {
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(r.Method + " " + r.URL.Path + " " + r.URL.Query().Get("source")))
	})
	adapter := New(httpHandler)

	response, err := adapter.Handle(context.Background(), events.APIGatewayV2HTTPRequest{
		RawPath:       "/items/Ada%20Lovelace",
		RawQueryString: "source=test",
		RequestContext: events.APIGatewayV2HTTPRequestContext{HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{Method: http.MethodPost, Path: "/items/123"}},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
	if response.Body != "POST /items/Ada Lovelace test" {
		t.Fatalf("body = %q, want %q", response.Body, "POST /items/Ada Lovelace test")
	}
}

func TestHandleRejectsInvalidBase64(t *testing.T) {
	adapter := New(http.NotFoundHandler())
	response, err := adapter.Handle(context.Background(), events.APIGatewayV2HTTPRequest{
		IsBase64Encoded: true,
		Body:            "%%%",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}
