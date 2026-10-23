GO ?= go
WAILS_VERSION := v2.17.0

.PHONY: desktop gatekeeper gatekeeper-linux connector-linux cagectl dev check test clean

GATEKEEPER_LINUX := internal/runtime/assets/gatekeeper/gatekeeper-linux-arm64
CONNECTOR_LINUX := internal/runtime/assets/connector/connector-linux-arm64

desktop: gatekeeper-linux connector-linux
	$(GO) build -tags production -trimpath -o bin/AIofCage .

gatekeeper:
	$(GO) build -trimpath -o bin/gatekeeper ./cmd/gatekeeper

# 沙箱里运行的门卫：Linux/arm64 静态二进制，编译进桌面应用和 cagectl，在专用虚拟机里打包成镜像。
gatekeeper-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(GATEKEEPER_LINUX) ./cmd/gatekeeper

# 可信连接器：代持数据库密码等秘密，运行需要秘密的 stdio MCP 服务，只供门卫调用。
connector-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w" -o $(CONNECTOR_LINUX) ./cmd/connector

# 命令行版本，与桌面应用共用运行时（开发、自动检查和演示兜底）。
cagectl: gatekeeper-linux connector-linux
	$(GO) build -trimpath -o bin/cagectl ./cmd/cagectl

dev:
	$(GO) run github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION) dev

check:
	$(GO) vet -tags production ./...
	$(GO) build -tags production ./...

test:
	$(GO) test -race ./internal/...

clean:
	rm -f bin/AIofCage bin/gatekeeper bin/cagectl $(GATEKEEPER_LINUX) $(CONNECTOR_LINUX)
