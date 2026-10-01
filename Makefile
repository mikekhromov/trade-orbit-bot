.PHONY: test lint build image start restart stop logs status deploy rollback

test:
	go test ./...

lint:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/telegram-bot ./cmd/bot

image:
	./shell-tools/build.sh

start:
	./shell-tools/start.sh

restart:
	./shell-tools/restart.sh

stop:
	./shell-tools/stop.sh

logs:
	./shell-tools/logs.sh

status:
	./shell-tools/status.sh

deploy:
	./shell-tools/deploy.sh

rollback:
	./shell-tools/rollback.sh "$(TAG)"
