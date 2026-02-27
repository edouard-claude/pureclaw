.PHONY: build build-arm64 test coverage vet clean

build:
	go build -o pureclaw ./cmd/pureclaw/

build-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o pureclaw-arm64 ./cmd/pureclaw/

test:
	go test -coverprofile=coverage.out ./...

coverage:
	go tool cover -func=coverage.out

vet:
	go vet ./...

clean:
	rm -f pureclaw pureclaw-arm64 coverage.out
