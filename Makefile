.PHONY: build test genkeys run run-node2 run-cluster clean docker-build docker-up docker-down docker-logs docker-ps docker-scale docker-load

build:
	go build -o main ./cmd/server

test:
	go test ./...

genkeys:
	go run ./tools/genkeys.go

prepare: genkeys build
	./main \
		--data-dir ./data/main \
		--api-addr :80 \
		--p2p-listen :9000 \
		--node-id main

run: build
	./main \
		--data-dir ./data/node1 \
		--api-addr :80 \
		--p2p-listen :90 \
		--node-id node-1 \
		--bolt-nosync

run-node2: build
	./main \
		--data-dir ./data/node2 \
		--api-addr :81 \
		--p2p-listen :91 \
		--node-id node-2 \
		--validator-key "" \
		--peer 127.0.0.1:90 \
		--bolt-nosync

run-cluster:
	@echo "Starting node 1 in background..."
	$(MAKE) run &
	@sleep 2
	@echo "Starting node 2..."
	$(MAKE) run-node2

clean:
	rm -rf bin data

COMPOSE ?= docker compose
PIXIE_URL ?= http://127.0.0.1

docker-build:
	$(COMPOSE) build

docker-up:
	$(COMPOSE) up -d --build

docker-down:
	$(COMPOSE) down

docker-logs:
	$(COMPOSE) logs -f --tail=100

docker-ps:
	$(COMPOSE) ps

docker-scale:
	@test -n "$(N)" || (echo "usage: make docker-scale N=3" && exit 1)
	$(COMPOSE) up -d --no-recreate --scale follower=$(N)

docker-load:
	go run ./tools/loadgen/... -api $(PIXIE_URL) -n $(or $(N),500) -workers $(or $(WORKERS),16)
