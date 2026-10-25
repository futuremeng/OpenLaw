# OpenLaw Makefile
# 国内网络建议: export GOPROXY=https://goproxy.cn,direct

.PHONY: test lint fmt vet check coverage build build-all

# 二进制构建（部署形态：CLI 单文件 + 数据目录，无常驻服务）
# 纯 Go（modernc.org/sqlite，无 CGO）→ 交叉编译即开即用
DIST := dist
CMDS := $(notdir $(wildcard cmd/*))
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

build:
	@mkdir -p $(DIST)
	@for c in $(CMDS); do \
		echo "build $$c"; \
		CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(DIST)/$$c ./cmd/$$c || exit 1; \
	done
	@echo "产物: $(DIST)/（当前平台 $(words $(CMDS)) 个 CLI）"

build-all:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		[ $$os = windows ] && ext=.exe; \
		mkdir -p $(DIST)/$$p; \
		for c in $(CMDS); do \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
				go build -trimpath -ldflags "-s -w" -o $(DIST)/$$p/$$c$$ext ./cmd/$$c || exit 1; \
		done; \
		echo "dist/$$p 完成（$(words $(CMDS)) 个 CLI）"; \
	done

test:
	go test ./... -v

lint:
	go vet ./...
	gofmt -l .
	@test -z "$$(gofmt -l .)" || (echo "请运行 make fmt"; exit 1)

fmt:
	gofmt -w .

vet:
	go vet ./...

# 日常运维入口（开发文档 9.2）：哈希重算 + manifest 签名校验 + 断链清单
check:
	go run ./cmd/openlaw-check -data data

coverage:
	go test ./... -cover
