.PHONY: build test install
build:
	go build -o bin/nook ./cmd/nook
test:
	go vet ./... && go test ./...
install: build
	install -m 0755 bin/nook /opt/homebrew/bin/nook 2>/dev/null || install -m 0755 bin/nook /usr/local/bin/nook
