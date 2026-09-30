package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"micro-api/internal/api"
	"micro-api/internal/lambdahttp"
)

func main() {
	handler := api.NewHandler()

	if os.Getenv("AWS_LAMBDA_RUNTIME_API") == "" {
		addr := ":" + envOr("PORT", "8888")
		log.Printf("starting local API on %s", addr)
		if err := http.ListenAndServe(addr, handler); err != nil {
			log.Fatal(err)
		}
		return
	}

	lambdaHandler := lambdahttp.New(handler)
	lambda.Start(func(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		return lambdaHandler.Handle(ctx, event)
	})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
