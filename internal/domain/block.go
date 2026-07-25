package domain

import "time"

type Block struct {
	Height         int64                      `json:"height"`
	Timestamp      time.Time                  `json:"timestamp"`
	Transactions   []PaymentTransaction       `json:"transactions"`
	AccountCreates []AccountCreateTransaction `json:"account_creates,omitempty"`
	AccountCloses  []AccountCloseTransaction  `json:"account_closes,omitempty"`
	PreviousHash   []byte                     `json:"previous_hash"`
	Hash           []byte                     `json:"hash"`
	Validator      string                     `json:"validator"`
	Signature      []byte                     `json:"signature,omitempty"`
}

type ChainInfo struct {
	ChainID    string   `json:"chain_id"`
	Height     int64    `json:"height"`
	Validators []string `json:"validators"`
}
