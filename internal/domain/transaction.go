package domain

import (
	"time"
)

type LineItem struct {
	Description string     `json:"description"`
	Amount      int64      `json:"amount"`
	TaxCodes    []TaxCode  `json:"tax_codes"`
	Discounts   []Discount `json:"discounts"`
}

type TaxCode string
type TaxSplit struct {
	RateBPS    int64     `json:"rate_bps"`
	TaxAccount AccountID `json:"tax_account"`
}

type Discount struct {
	Code       string    `json:"code"`
	Amount     int64     `json:"amount"`
	TaxAccount AccountID `json:"tax_account"`
}

type PaymentTransaction struct {
	ID        string     `json:"id"`
	Timestamp time.Time  `json:"timestamp"`
	Payer     Account    `json:"payer"`
	Payee     Account    `json:"payee"`
	Items     []LineItem `json:"items"`
	Signature []byte     `json:"signature,omitempty"`
}

// AccountCreateTransaction registers a new person/merchant account on-chain.
// Balance in Account must be 0; Signature is from a genesis validator.
type AccountCreateTransaction struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Account   Account   `json:"account"`
	PublicKey string    `json:"public_key"`
	Signature []byte    `json:"signature,omitempty"`
}

// AccountCloseTransaction hard-deletes a person/merchant account on-chain.
// Person: validator-signed; residual balance goes to Destination (or tax treasury).
// Merchant: account-signed; balance must be 0.
type AccountCloseTransaction struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	AccountID   AccountID `json:"account_id"`
	PublicKey   string    `json:"public_key"`
	Destination AccountID `json:"destination,omitempty"`
	Signature   []byte    `json:"signature,omitempty"`
}

func (tx *PaymentTransaction) GrossAmount() int64 {
	var total int64
	for _, item := range tx.Items {
		total += item.Amount
	}
	return total
}

func (tx *PaymentTransaction) TaxTotal(taxes map[TaxCode]TaxSplit) int64 {
	var total int64
	for _, item := range tx.Items {
		total += item.TaxTotal(taxes)
	}
	return total
}

func (tx *PaymentTransaction) DiscountTotal() int64 {
	var total int64
	for _, item := range tx.Items {
		total += item.DiscountTotal()
	}
	return total
}

func (tx *PaymentTransaction) NetToPayee(taxes map[TaxCode]TaxSplit) int64 {
	return tx.GrossAmount() - tx.TaxTotal(taxes) - tx.DiscountTotal()
}

func (i *LineItem) TaxTotal(taxes map[TaxCode]TaxSplit) int64 {
	var total int64
	for _, taxCode := range i.TaxCodes {
		taxSplit, ok := taxes[taxCode]
		if !ok {
			continue
		}
		total += taxSplit.RateBPS * i.Amount / 10000
	}
	return total
}

func (i *LineItem) DiscountTotal() int64 {
	var total int64
	for _, discount := range i.Discounts {
		total += discount.Amount
	}
	return total
}
