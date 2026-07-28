#!/bin/sh
set -e

ROLE="${PIXIE_ROLE:-follower}"
NODE_ID="${PIXIE_NODE_ID:-$(hostname)}"
CONFIG_DIR="${PIXIE_CONFIG_DIR:-/config}"
DATA_DIR="${PIXIE_DATA_DIR:-/data}"
PEER="${PIXIE_PEER:-validator:90}"
BOLT_NOSYNC="${PIXIE_BOLT_NOSYNC:-}"

mkdir -p "$DATA_DIR" "$CONFIG_DIR"

if [ "$ROLE" = "validator" ]; then
	if [ ! -f "$CONFIG_DIR/genesis.json" ]; then
		echo "Generating genesis and keys in $CONFIG_DIR ..."
		cd /app
		./genkeys
		cp config/genesis.json config/keystore.json config/validator-key.json "$CONFIG_DIR/"
	fi
	VALIDATOR_KEY="$CONFIG_DIR/validator-key.json"
	exec /app/pixie \
		--data-dir "$DATA_DIR" \
		--genesis "$CONFIG_DIR/genesis.json" \
		--keystore "$CONFIG_DIR/keystore.json" \
		--validator-key "$VALIDATOR_KEY" \
		--taxes /app/config/taxes.json \
		--api-addr ":80" \
		--p2p-listen ":90" \
		--node-id "$NODE_ID" \
		$([ -n "$BOLT_NOSYNC" ] && echo --bolt-nosync)
fi

# Follower: wait until validator published shared config.
i=0
while [ ! -f "$CONFIG_DIR/genesis.json" ]; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "timed out waiting for genesis at $CONFIG_DIR/genesis.json" >&2
		exit 1
	fi
	echo "waiting for shared config..."
	sleep 2
done

exec /app/pixie \
	--data-dir "$DATA_DIR" \
	--genesis "$CONFIG_DIR/genesis.json" \
	--keystore "$CONFIG_DIR/keystore.json" \
	--validator-key "" \
	--taxes /app/config/taxes.json \
	--api-addr ":80" \
	--p2p-listen ":90" \
	--node-id "$NODE_ID" \
	--peer "$PEER" \
	$([ -n "$BOLT_NOSYNC" ] && echo --bolt-nosync)
