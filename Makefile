.PHONY: build build-pi build-arm64 test coverage vet clean release-dry

VERSION ?= dev

build:
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=$(VERSION)" -o pureclaw ./cmd/pureclaw/

build-pi:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w -X main.Version=$(VERSION)" -o pureclaw-arm7 ./cmd/pureclaw/

build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "-s -w -X main.Version=$(VERSION)" -o pureclaw-arm64 ./cmd/pureclaw/

test:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...

coverage: test
	go tool cover -func=coverage.out

vet:
	go vet ./...

release-dry:
	goreleaser release --snapshot --clean

clean:
	rm -f pureclaw pureclaw-arm7 pureclaw-arm64 coverage.out
