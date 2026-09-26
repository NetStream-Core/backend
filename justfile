build:
    go build ./...

test:
    go test ./...

vet:
    go vet ./...

lint:
    golangci-lint run ./...

run:
    go run ./cmd
