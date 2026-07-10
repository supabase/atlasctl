BINARY     := atlasctl
BUILD_DIR  := dist
MODULE     := github.com/supabase/atlasctl
CMD        := ./cmd/atlasctl

GOFLAGS    ?=
LDFLAGS    := -s -w

.PHONY: build test vet lint clean

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
	rm -rf $(BUILD_DIR)
