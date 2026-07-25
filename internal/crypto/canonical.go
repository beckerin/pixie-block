package crypto

import (
	"encoding/json"
	"fmt"

	"github.com/beckerin/pixie-block/internal/domain"
)

type unsignedTransaction struct {
	ID        string            `json:"id"`
	Timestamp string            `json:"timestamp"`
	Payer     domain.Account    `json:"payer"`
	Payee     domain.Account    `json:"payee"`
	Currency  string            `json:"currency"`
	Items     []domain.LineItem `json:"items"`
}

type unsignedAccountCreate struct {
	ID        string         `json:"id"`
	Timestamp string         `json:"timestamp"`
	Account   domain.Account `json:"account"`
	PublicKey string         `json:"public_key"`
}

type unsignedBlock struct {
	Height         int64                   `json:"height"`
	Timestamp      string                  `json:"timestamp"`
	Transactions   []unsignedTransaction   `json:"transactions"`
	AccountCreates []unsignedAccountCreate `json:"account_creates,omitempty"`
	PreviousHash   string                  `json:"previous_hash"`
	Validator      string                  `json:"validator"`
}

func TransactionSignBytes(tx domain.PaymentTransaction) ([]byte, error) {
	payload := unsignedTransaction{
		ID:        tx.ID,
		Timestamp: tx.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		Payer:     tx.Payer,
		Payee:     tx.Payee,
		Currency:  tx.Currency,
		Items:     tx.Items,
	}
	return json.Marshal(payload)
}

func AccountCreateSignBytes(tx domain.AccountCreateTransaction) ([]byte, error) {
	payload := unsignedAccountCreate{
		ID:        tx.ID,
		Timestamp: tx.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		Account:   tx.Account,
		PublicKey: tx.PublicKey,
	}
	return json.Marshal(payload)
}

func BlockSignBytes(block domain.Block) ([]byte, error) {
	txs := make([]unsignedTransaction, len(block.Transactions))
	for i, tx := range block.Transactions {
		txs[i] = unsignedTransaction{
			ID:        tx.ID,
			Timestamp: tx.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Payer:     tx.Payer,
			Payee:     tx.Payee,
			Currency:  tx.Currency,
			Items:     tx.Items,
		}
	}

	creates := make([]unsignedAccountCreate, len(block.AccountCreates))
	for i, tx := range block.AccountCreates {
		creates[i] = unsignedAccountCreate{
			ID:        tx.ID,
			Timestamp: tx.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Account:   tx.Account,
			PublicKey: tx.PublicKey,
		}
	}

	payload := unsignedBlock{
		Height:         block.Height,
		Timestamp:      block.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		Transactions:   txs,
		AccountCreates: creates,
		PreviousHash:   fmt.Sprintf("%x", block.PreviousHash),
		Validator:      block.Validator,
	}
	return json.Marshal(payload)
}
