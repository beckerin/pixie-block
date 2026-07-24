.PHONY: build test genkeys run run-node2 run-cluster clean

build:
	go build -o bin/pixie-node ./cmd/node

test:
	go test ./...

genkeys:
	go run ./tools/genkeys.go

run: build
	./bin/pixie-node \
		--data-dir ./data/node1 \
		--api-addr :8080 \
		--p2p-listen :9000 \
		--node-id node-1

run-node2: build
	./bin/pixie-node \
		--data-dir ./data/node2 \
		--api-addr :8081 \
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
