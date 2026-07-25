.PHONY: build test genkeys run run-node2 run-cluster clean

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
		--p2p-listen :9000 \
		--node-id node-1

run-node2: build
	./main \
		--data-dir ./data/node2 \
		--api-addr :81 \
		--p2p-listen :9001 \
		--node-id node-2 \
		--peer 127.0.0.1:9000

run-cluster:
	@echo "Starting node 1 in background..."
	$(MAKE) run &
	@sleep 2
	@echo "Starting node 2..."
	$(MAKE) run-node2

clean:
	rm -rf bin data
