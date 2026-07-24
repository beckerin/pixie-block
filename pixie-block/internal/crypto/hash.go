package crypto

import (
	"crypto/sha256"
	"encoding/hex"
)

func SHA256(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func SHA256Hex(data []byte) string {
	return hex.EncodeToString(SHA256(data))
}
