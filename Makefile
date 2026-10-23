GO ?= go
WAILS_VERSION := v2.17.0

.PHONY: desktop gatekeeper gatekeeper-linux dev check test clean

desktop:
	$(GO) build -tags production -trimpath -o bin/AIofCage .

gatekeeper:
	$(GO) build -trimpath -o bin/gatekeeper ./cmd/gatekeeper

# 沙箱里运行的门卫：Linux/arm64 静态二进制，供 docker/gatekeeper/Dockerfile 打包。
gatekeeper-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -o build/gatekeeper/gatekeeper ./cmd/gatekeeper
	cp docker/gatekeeper/Dockerfile build/gatekeeper/Dockerfile

dev:
	$(GO) run github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION) dev

check:
	$(GO) vet -tags production ./...
	$(GO) build -tags production ./...

test:
	$(GO) test -race ./internal/...

clean:
	rm -f bin/AIofCage bin/gatekeeper
	rm -rf build/gatekeeper
