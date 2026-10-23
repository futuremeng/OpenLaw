# OpenLaw Makefile
# 国内网络建议: export GOPROXY=https://goproxy.cn,direct

.PHONY: test lint fmt vet check coverage

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
