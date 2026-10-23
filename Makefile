BINARY  := usagi
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run test vet fmt tidy install uninstall clean

all: build

## build: 编译静态二进制（无 cgo）
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

## run: 前台编译并启动（不开浏览器，自己访问打印出来的地址）
run: build
	./$(BINARY)

## test: 单元测试
test:
	go test ./... -count=1

## vet: 静态检查
vet:
	go vet ./...

## fmt: 格式化
fmt:
	gofmt -l -w .

## install: 装到 ~/.local/bin + udev 规则 + systemd 用户服务
install:
	./install.sh

## uninstall: 卸载
uninstall:
	./uninstall.sh

clean:
	rm -f $(BINARY)
