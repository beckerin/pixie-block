package poa

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
)

type Producer struct {
	genesis    config.Genesis
	validator  config.ValidatorConfig
	privateKey ed25519.PrivateKey
}

func NewProducer(genesis config.Genesis, validatorID, privateKeyB64 string) (*Producer, error) {
	var validator config.ValidatorConfig
	found := false
	for _, v := range genesis.Validators {
		if v.ID == validatorID {
			validator = v
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("validator %q not in genesis", validatorID)
	}

	priv, err := crypto.ParsePrivateKey(privateKeyB64)
	if err != nil {
		return nil, err
	}

	pub := priv.Public().(ed25519.PublicKey)
	expectedPub, err := crypto.ParsePublicKey(validator.PublicKey)
	if err != nil {
		return nil, err
	}
	if string(pub) != string(expectedPub) {
		return nil, fmt.Errorf("validator private key does not match genesis public key")
	}

	return &Producer{
		genesis:    genesis,
		validator:  validator,
		privateKey: priv,
	}, nil
}

func (p *Producer) ValidatorForHeight(height int64) config.ValidatorConfig {
	idx := int(height % int64(len(p.genesis.Validators)))
	return p.genesis.Validators[idx]
}

func (p *Producer) CanProduce(height int64) bool {
	return p.ValidatorForHeight(height).ID == p.validator.ID
}

func (p *Producer) CreateBlock(height int64, prev domain.Block, txs []domain.PaymentTransaction, creates []domain.AccountCreateTransaction, closes []domain.AccountCloseTransaction) (domain.Block, error) {
	if !p.CanProduce(height) {
		return domain.Block{}, fmt.Errorf("validator %q cannot produce block at height %d", p.validator.ID, height)
	}

	block := domain.Block{
		Height:         height,
		Timestamp:      time.Now().UTC(),
		Transactions:   txs,
		AccountCreates: creates,
		AccountCloses:  closes,
		PreviousHash:   prev.Hash,
		Validator:      p.validator.ID,
	}

	hash, err := crypto.BlockHash(block)
	if err != nil {
		return domain.Block{}, err
	}
	block.Hash = hash

	signBytes, err := crypto.BlockSignBytes(block)
	if err != nil {
		return domain.Block{}, err
	}
	block.Signature = crypto.Sign(p.privateKey, signBytes)

	return block, nil
}
