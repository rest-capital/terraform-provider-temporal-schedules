default: build

build:
	go build -v ./...

install:
	go install -v ./...

test:
	go test -v -cover -timeout=120s ./...

testacc:
	TF_ACC=1 go test -v -cover -count=1 -timeout 10m ./...

lint:
	golangci-lint run

fmt:
	gofmt -s -w -e .

.PHONY: build install test testacc lint fmt
