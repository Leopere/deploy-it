.PHONY: test build install

test:
	go test ./...

build:
	go build -trimpath -o deploy-it ./cmd/deploy-it

install: build
	./deploy-it install
