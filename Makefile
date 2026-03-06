.PHONY: build run test lint coverage clean

build:
	go build -o bin/claw ./cmd/claw

run:
	go run ./cmd/claw -config configs/config.yaml

test:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf bin/ coverage.out coverage.html
