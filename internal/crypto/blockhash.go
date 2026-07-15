package crypto

import (
	"github.com/solidk-tech/pixie-block/internal/domain"
)

func BlockHash(block domain.Block) ([]byte, error) {
	signBytes, err := BlockSignBytes(block)
	if err != nil {
		return nil, err
	}
	return SHA256(signBytes), nil
}
