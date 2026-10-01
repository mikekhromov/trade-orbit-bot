.PHONY: test lint build image deploy

test:
	go test ./...

lint:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/telegram-bot ./cmd/bot

image:
	./shell-tools/build.sh

deploy:
	./shell-tools/deploy.sh
