package ledger

import (
	"crypto/ed25519"
	"fmt"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
)

type State struct {
	Accounts         map[domain.AccountID]domain.Account
	TaxSplit         map[domain.TaxCode]domain.TaxSplit
	TaxTreasury      domain.AccountID
	AllowedTaxAccts  map[domain.AccountID]struct{}
	ValidatorPubKeys map[string]ed25519.PublicKey
	accountPubKeys   map[string]ed25519.PublicKey
}

func NewState(taxTreasury domain.AccountID, allowedTax []domain.AccountID, taxes config.Taxes) *State {
	allowed := make(map[domain.AccountID]struct{}, len(allowedTax))
	for _, id := range allowedTax {
		allowed[id] = struct{}{}
	}
	return &State{
		Accounts:         make(map[domain.AccountID]domain.Account),
		TaxSplit:         taxes.TaxSplit,
		TaxTreasury:      taxTreasury,
		AllowedTaxAccts:  allowed,
		ValidatorPubKeys: make(map[string]ed25519.PublicKey),
		accountPubKeys:   make(map[string]ed25519.PublicKey),
	}
}

func (s *State) Clone() *State {
	clone := NewState(s.TaxTreasury, nil, config.Taxes{TaxSplit: s.TaxSplit})
	for id := range s.AllowedTaxAccts {
		clone.AllowedTaxAccts[id] = struct{}{}
	}
	for id, acct := range s.Accounts {
		clone.Accounts[id] = acct
	}
	for id, pk := range s.ValidatorPubKeys {
		clone.ValidatorPubKeys[id] = pk
	}
	for taxCode, split := range s.TaxSplit {
		clone.TaxSplit[taxCode] = split
	}
	if s.accountPubKeys != nil {
		for id, pk := range s.accountPubKeys {
			clone.accountPubKeys[id] = pk
		}
	}
	return clone
}

func (s *State) SetBalance(id domain.AccountID, balance int64, currency string) {
	existing, ok := s.Accounts[id]
	acct := domain.Account{
		ID:       id,
		Balance:  balance,
		Currency: currency,
	}
	if ok {
		acct.Type = existing.Type
	}
	s.Accounts[id] = acct
}

func (s *State) SetAccount(acct domain.Account) {
	s.Accounts[acct.ID] = acct
}

func (s *State) Balance(id domain.AccountID) (int64, bool) {
	acct, ok := s.Accounts[id]
	if !ok {
		return 0, false
	}
	return acct.Balance, true
}

func (s *State) AddValidator(id string, pub ed25519.PublicKey) {
	s.ValidatorPubKeys[id] = pub
}

func ValidateTransaction(tx domain.PaymentTransaction, state *State) error {
	if tx.ID == "" {
		return fmt.Errorf("transaction id is required")
	}
	if tx.Payer.ID == "" || tx.Payee.ID == "" {
		return fmt.Errorf("payer and payee are required")
	}
	if tx.Currency == "" {
		return fmt.Errorf("currency is required")
	}
	if len(tx.Items) == 0 {
		return fmt.Errorf("at least one line item is required")
	}
	for _, item := range tx.Items {
		if item.Amount <= 0 {
			return fmt.Errorf("line item amount must be positive")
		}
		for _, taxCode := range item.TaxCodes {
			split, ok := state.TaxSplit[taxCode]
			if !ok {
				return fmt.Errorf("unknown tax code %q", taxCode)
			}
			if _, ok := state.AllowedTaxAccts[split.TaxAccount]; !ok {
				return fmt.Errorf("tax account %q is not allowed", split.TaxAccount)
			}
		}

		for _, discount := range item.Discounts {
			if discount.Amount < 0 {
				return fmt.Errorf("discount amount cannot be negative")
			}
			if _, ok := state.AllowedTaxAccts[discount.TaxAccount]; !ok {
				return fmt.Errorf("discount tax account %q is not allowed", discount.TaxAccount)
			}
		}
	}

	gross := tx.GrossAmount()
	if gross <= 0 {
		return fmt.Errorf("gross amount must be positive")
	}

	taxTotal := tx.TaxTotal(state.TaxSplit)
	discountTotal := tx.DiscountTotal()
	net := tx.NetToPayee(state.TaxSplit)

	if taxTotal+discountTotal+net != gross {
		return fmt.Errorf("split mismatch: taxes(%d) + discounts(%d) + net(%d) != gross(%d)",
			taxTotal, discountTotal, net, gross)
	}
	if net < 0 {
		return fmt.Errorf("net to payee cannot be negative")
	}

	payerBalance, ok := state.Balance(tx.Payer.ID)
	if !ok {
		return fmt.Errorf("payer account %q not found", tx.Payer.ID)
	}
	if payerBalance < gross {
		return fmt.Errorf("insufficient balance: have %d need %d", payerBalance, gross)
	}

	payeeAcct, ok := state.Accounts[tx.Payee.ID]
	if !ok {
		return fmt.Errorf("payee account %q not found", tx.Payee.ID)
	}
	if payeeAcct.Currency != tx.Currency {
		return fmt.Errorf("payee currency mismatch")
	}

	if len(tx.Signature) == 0 {
		return fmt.Errorf("transaction signature is required")
	}

	signBytes, err := crypto.TransactionSignBytes(tx)
	if err != nil {
		return fmt.Errorf("canonical tx bytes: %w", err)
	}

	payerPub, ok := state.AccountPubKey(string(tx.Payer.ID))
	if !ok {
		return fmt.Errorf("payer public key not found for %q", tx.Payer.ID)
	}
	if !crypto.Verify(payerPub, signBytes, tx.Signature) {
		return fmt.Errorf("invalid transaction signature")
	}

	return nil
}

func (s *State) AccountPubKey(accountID string) (ed25519.PublicKey, bool) {
	if s.accountPubKeys == nil {
		return nil, false
	}
	pk, ok := s.accountPubKeys[accountID]
	return pk, ok
}

func (s *State) SetAccountPubKey(accountID string, pub ed25519.PublicKey) {
	if s.accountPubKeys == nil {
		s.accountPubKeys = make(map[string]ed25519.PublicKey)
	}
	s.accountPubKeys[accountID] = pub
}

// ApplyTransaction mutates state with a validated transaction.
func ApplyTransaction(tx domain.PaymentTransaction, state *State) error {
	if err := ValidateTransaction(tx, state); err != nil {
		return err
	}

	gross := tx.GrossAmount()
	net := tx.NetToPayee(state.TaxSplit)

	payer := state.Accounts[tx.Payer.ID]
	payer.Balance -= gross
	state.Accounts[tx.Payer.ID] = payer

	payee := state.Accounts[tx.Payee.ID]
	payee.Balance += net
	state.Accounts[tx.Payee.ID] = payee

	for _, item := range tx.Items {
		for _, taxCode := range item.TaxCodes {
			split, ok := state.TaxSplit[taxCode]
			if !ok {
				return fmt.Errorf("unknown tax code %q", taxCode)
			}

			taxAcct := state.Accounts[split.TaxAccount]
			taxAcct.Balance += split.RateBPS * item.Amount / 10000
			state.Accounts[split.TaxAccount] = taxAcct
		}

		for _, discount := range item.Discounts {
			if discount.Amount == 0 {
				continue
			}
			taxAcct := state.Accounts[discount.TaxAccount]
			taxAcct.Balance += discount.Amount
			state.Accounts[discount.TaxAccount] = taxAcct
		}
	}

	return nil
}

func ValidateBlock(block domain.Block, prev domain.Block, state *State) (*State, error) {
	if block.Height != prev.Height+1 {
		return nil, fmt.Errorf("invalid block height: expected %d got %d", prev.Height+1, block.Height)
	}
	if string(block.PreviousHash) != string(prev.Hash) {
		return nil, fmt.Errorf("previous hash mismatch")
	}

	computedHash, err := crypto.BlockHash(block)
	if err != nil {
		return nil, err
	}
	if string(block.Hash) != string(computedHash) {
		return nil, fmt.Errorf("block hash mismatch")
	}

	pub, ok := state.ValidatorPubKeys[block.Validator]
	if !ok {
		return nil, fmt.Errorf("unknown validator %q", block.Validator)
	}

	signBytes, err := crypto.BlockSignBytes(block)
	if err != nil {
		return nil, fmt.Errorf("canonical block bytes: %w", err)
	}
	if len(block.Signature) == 0 || !crypto.Verify(pub, signBytes, block.Signature) {
		return nil, fmt.Errorf("invalid block signature")
	}

	nextState := state.Clone()
	for _, tx := range block.Transactions {
		if err := ApplyTransaction(tx, nextState); err != nil {
			return nil, fmt.Errorf("tx %s: %w", tx.ID, err)
		}
	}

	return nextState, nil
}
