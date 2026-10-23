GO ?= go
WAILS_VERSION := v2.17.0

.PHONY: desktop gatekeeper dev check test clean

desktop:
	$(GO) build -tags production -trimpath -o bin/AIofCage .

gatekeeper:
	$(GO) build -trimpath -o bin/gatekeeper ./cmd/gatekeeper

dev:
	$(GO) run github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION) dev

check:
	$(GO) vet -tags production ./...
	$(GO) build -tags production ./...

test:
	$(GO) test -race ./internal/...

clean:
	rm -f bin/AIofCage bin/gatekeeper
