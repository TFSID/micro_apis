BINARY := bootstrap

.PHONY: run test build deploy clean

run:
	go run ./cmd/api

test:
	go test ./...

build:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(BINARY) ./cmd/api
	mkdir -p .build
	chmod 755 $(BINARY)
	zip -X -j .build/micro-api.zip $(BINARY)

deploy: build
	npx serverless deploy

clean:
	go clean
	$(RM) $(BINARY)
