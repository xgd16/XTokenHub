-include Makefile.local

GO        ?= go
PNPM      ?= pnpm
BIN_DIR   := bin
DIST_DIR  := dist
BACKEND_PORT ?= 8080

# ---------- 编译参数：默认构建本机平台；交叉编译时用变量覆盖 ----------
#   例: make build GOOS=linux GOARCH=arm64
#       make compile GOOS=linux GOARCH=amd64 CGO_ENABLED=0
GOOS        ?= $(shell $(GO) env GOOS)
GOARCH      ?= $(shell $(GO) env GOARCH)
# sqlite 为 modernc.org/sqlite（纯 Go），CGO_ENABLED=0 即可静态交叉编译，
# 产物不依赖 glibc/musl 与 C 工具链，可直接跑 postmarketOS / Alpine 等系统。
CGO_ENABLED ?= 0

HOST_OS   := $(shell $(GO) env GOOS)
HOST_ARCH := $(shell $(GO) env GOARCH)
# 非本机平台时产物名带平台后缀，避免覆盖本机二进制
PLAT_SUFFIX := $(if $(filter $(GOOS)/$(GOARCH),$(HOST_OS)/$(HOST_ARCH)),,-$(GOOS)-$(GOARCH))
SERVER_BIN  := $(BIN_DIR)/xtokenhub$(PLAT_SUFFIX)

# ---------- 版本信息：注入 main.version/main.commit，运行 ./xtokenhub -version 可查 ----------
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# dist 目标打包的平台矩阵
DIST_PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: dev server-dev web-dev web server compile build test cover web-test lint tidy clean dist dist-pmos

## dev: 并行启动后端(8080)与前端 Vite dev server(5173, 代理 /api /v1)
dev:
	@$(MAKE) -j2 server-dev web-dev

server-dev:
	$(GO) run ./cmd/server -config configs/config.yaml

web-dev:
	cd frontend && $(PNPM) dev

## web: 构建前端产物到 web/dist（供 go:embed）
web:
	cd frontend && $(PNPM) build

## server: 快速编译（不重建前端、不带裁剪，便于本地调试）
server:
	$(GO) build -o $(SERVER_BIN) ./cmd/server
	@echo "==> $(SERVER_BIN) 构建完成"

## compile: 仅编译静态二进制（不重建前端），可用 GOOS/GOARCH 交叉编译
compile:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(SERVER_BIN) ./cmd/server
	@echo "==> $(SERVER_BIN) 构建完成 ($(GOOS)/$(GOARCH) 静态) v$(VERSION) ($(COMMIT))"

## build: 构建前端 + 编译静态单二进制（内嵌前端产物）
build: web compile

## dist: 交叉打包常见平台到 dist/（每平台静态单文件 + tar.gz，含前端内嵌）
dist: web
	@mkdir -p $(DIST_DIR)
	@set -e; for p in $(DIST_PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; name=xtokenhub-$$os-$$arch; \
	  echo "==> $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$$name ./cmd/server; \
	  tar -C $(DIST_DIR) -czf $(DIST_DIR)/$$name.tar.gz $$name; \
	done; \
	ls -lh $(DIST_DIR)/xtokenhub-*

## dist-pmos: 为手机打包 —— postmarketOS / Alpine（aarch64 / musl）
##   静态单文件，无需任何依赖；上传后 chmod +x 即可运行
dist-pmos: web
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/xtokenhub-pmos-aarch64 ./cmd/server
	tar -C $(DIST_DIR) -czf $(DIST_DIR)/xtokenhub-pmos-aarch64.tar.gz xtokenhub-pmos-aarch64
	@echo "==> $(DIST_DIR)/xtokenhub-pmos-aarch64.tar.gz 打包完成"
	@echo "    上传: scp $(DIST_DIR)/xtokenhub-pmos-aarch64 <设备>:/usr/local/bin/xtokenhub"
	@echo "    验证: ssh <设备> xtokenhub -version"

## test: 全量后端测试（含 race 检测）
test:
	$(GO) test ./... -race -count=1

## cover: 覆盖率报告 -> coverage.html（测试在 internal/tests，需 -coverpkg 指回被测包）
cover:
	$(GO) test ./... -race -count=1 -coverpkg=./internal/... -covermode=atomic -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html
	$(GO) tool cover -func=coverage.out | tail -1

## web-test: 前端 Vitest 测试
web-test:
	cd frontend && $(PNPM) test run

## lint: go vet
lint:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out coverage.html
