GOPROXY ?= https://goproxy.cn,direct
WAILS ?= $(shell go env GOPATH)/bin/wails

.PHONY: help tidy test run fmt frontend-install frontend-build dev build

help: ## 显示可用命令
	@echo "Usage: make <target>"
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

tidy: ## 整理 Go 依赖
	GOPROXY=$(GOPROXY) go mod tidy

fmt: ## 格式化 Go 代码
	gofmt -w $$(rg --files -g '*.go')

test: ## 运行全部 Go 测试
	GOPROXY=$(GOPROXY) go test ./...

run: ## 启动 Go 主程序
	GOPROXY=$(GOPROXY) go run .

frontend-install: ## 安装前端依赖
	cd frontend && npm install

frontend-build: ## 构建前端静态资源
	cd frontend && npm run build

dev: ## 启动 Wails 开发模式
	$(WAILS) dev

build: ## 打包桌面应用
	GOPROXY=$(GOPROXY) $(WAILS) build
