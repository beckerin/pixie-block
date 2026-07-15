package domain

import "time"

type LineItem struct {
	Description string `json:"description"`
	Amount      int64  `json:"amount"`
}

type TaxSplit struct {
	TaxCode    string    `json:"tax_code"`
	RateBPS    int64     `json:"rate_bps"`
	Amount     int64     `json:"amount"`
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
	Payer     AccountID  `json:"payer"`
	Payee     AccountID  `json:"payee"`
	Currency  string     `json:"currency"`
	Items     []LineItem `json:"items"`
	TaxSplits []TaxSplit `json:"tax_splits"`
	Discounts []Discount `json:"discounts"`
	Signature []byte     `json:"signature,omitempty"`
}

func (tx *PaymentTransaction) GrossAmount() int64 {
	var total int64
	for _, item := range tx.Items {
		total += item.Amount
	}
	return total
}

func (tx *PaymentTransaction) TaxTotal() int64 {
	var total int64
	for _, split := range tx.TaxSplits {
		total += split.Amount
	}
	return total
}

func (tx *PaymentTransaction) DiscountTotal() int64 {
	var total int64
	for _, d := range tx.Discounts {
		total += d.Amount
	}
	return total
}

func (tx *PaymentTransaction) NetToPayee() int64 {
	return tx.GrossAmount() - tx.TaxTotal() - tx.DiscountTotal()
}
