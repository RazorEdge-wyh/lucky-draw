# lucky-draw 常用命令
#
# Windows 上没有 make 时，直接用注释里的等价 go 命令即可。

BINARY  := lucky-draw
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help
help: ## 显示所有可用目标
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## 本地启动服务（默认 :8080）
	go run .

.PHONY: build
build: ## 编译到 build/ 目录
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o build/$(BINARY) .

.PHONY: test
test: ## 运行全部测试
	go test ./...

.PHONY: test-race
test-race: ## 运行测试并开启竞态检测
	go test -race -count=1 ./...

.PHONY: cover
cover: ## 生成 HTML 覆盖率报告
	go test -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "已生成 coverage.html"

.PHONY: fmt
fmt: ## 格式化代码
	gofmt -w .

.PHONY: vet
vet: ## 静态检查
	go vet ./...

.PHONY: check
check: fmt vet test ## 提交前自检：格式化 + 静态检查 + 测试

.PHONY: bench
bench: ## 运行基准测试
	go test -bench=. -benchmem ./internal/engine/

.PHONY: docker
docker: ## 构建容器镜像
	docker build --build-arg VERSION=$(VERSION) -t lucky-draw:$(VERSION) .

.PHONY: compose
compose: ## 用 docker compose 启动
	docker compose up --build -d

.PHONY: clean
clean: ## 清理构建产物
	rm -rf build coverage.out coverage.html
