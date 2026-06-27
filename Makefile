# flist Makefile

BINARY := flist
CMD := ./cmd/flist
FRONTEND := frontend
WEB_DIST := web/dist

.PHONY: all build build-frontend embed-frontend backend \
        build-linux build-windows build-all \
        run test vet tidy clean

all: build

## 完整生产构建：前端 build → 复制到 web/dist → 后端 build
build: embed-frontend backend

build-frontend:
	cd $(FRONTEND) && npm install && npm run build

## 用前端产物覆盖嵌入目录
embed-frontend: build-frontend
	rm -rf $(WEB_DIST)
	cp -r $(FRONTEND)/dist $(WEB_DIST)

backend:
	go build -o $(BINARY) $(CMD)

## 交叉编译（纯 Go SQLite，无需额外工具链）
build-linux: embed-frontend
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 $(CMD)

build-windows: embed-frontend
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY)-windows-amd64.exe $(CMD)

build-all: build-linux build-windows

## 仅后端开发运行（使用仓库已提交的前端产物）
run:
	go run $(CMD) --root ./testroot --admin-pass test1234

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
	rm -rf dist
