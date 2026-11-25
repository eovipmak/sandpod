BINDIR ?= /usr/local/bin
BIN := sandpodctl

.PHONY: build test vet cross install clean

build:
	go build -o $(BIN) ./cmd/sandpodctl

test:
	go test ./...

vet:
	go vet ./...

# Cross-compile check (Linux-first, macOS dev supported, Windows best-effort).
cross:
	GOOS=linux GOARCH=amd64 go build -o /tmp/sandpodctl-linux ./cmd/sandpodctl
	GOOS=darwin GOARCH=arm64 go build -o /tmp/sandpodctl-darwin ./cmd/sandpodctl
	GOOS=windows GOARCH=amd64 go build -o /tmp/sandpodctl.exe ./cmd/sandpodctl

install: build
	install -m 0755 $(BIN) $(BINDIR)/$(BIN)

clean:
	rm -f $(BIN)
