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
	AllowedTaxAccts  map[domain.AccountID]struct{}
	ValidatorPubKeys map[string]ed25519.PublicKey
	accountPubKeys   map[string]ed25519.PublicKey
}

func NewState(allowedTax []domain.AccountID, taxes config.Taxes) *State {
	allowed := make(map[domain.AccountID]struct{}, len(allowedTax))
	for _, id := range allowedTax {
		allowed[id] = struct{}{}
	}
	taxSplit := make(map[domain.TaxCode]domain.TaxSplit, len(taxes.TaxSplit))
	for code, split := range taxes.TaxSplit {
		taxSplit[code] = split
	}
	return &State{
		Accounts:         make(map[domain.AccountID]domain.Account),
		TaxSplit:         taxSplit,
		AllowedTaxAccts:  allowed,
		ValidatorPubKeys: make(map[string]ed25519.PublicKey),
		accountPubKeys:   make(map[string]ed25519.PublicKey),
	}
}

func (s *State) Clone() *State {
	clone := NewState(nil, config.Taxes{TaxSplit: s.TaxSplit})
	for id := range s.AllowedTaxAccts {
		clone.AllowedTaxAccts[id] = struct{}{}
	}
	for id, acct := range s.Accounts {
		clone.Accounts[id] = acct
	}
	for id, pk := range s.ValidatorPubKeys {
		clone.ValidatorPubKeys[id] = pk
	}
	if s.accountPubKeys != nil {
		for id, pk := range s.accountPubKeys {
			clone.accountPubKeys[id] = pk
		}
	}
	return clone
}

func (s *State) SetBalance(id domain.AccountID, balance int64) {
	existing, ok := s.Accounts[id]
	acct := domain.Account{
		ID:      id,
		Balance: balance,
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

func (s *State) RemoveAccountPubKey(accountID string) {
	if s.accountPubKeys == nil {
		return
	}
	delete(s.accountPubKeys, accountID)
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

func ValidateAccountCreate(tx domain.AccountCreateTransaction, state *State) error {
	if tx.ID == "" {
		return fmt.Errorf("account create id is required")
	}
	if tx.Account.ID == "" {
		return fmt.Errorf("account id is required")
	}
	switch tx.Account.Type {
	case domain.AccountTypePerson, domain.AccountTypeMerchant:
		// ok
	default:
		return fmt.Errorf("account type %q is not allowed; must be person or merchant", tx.Account.Type)
	}
	if tx.Account.Balance != 0 {
		return fmt.Errorf("new account balance must be 0")
	}
	if _, exists := state.Accounts[tx.Account.ID]; exists {
		return fmt.Errorf("account %q already exists", tx.Account.ID)
	}
	if tx.PublicKey == "" {
		return fmt.Errorf("public key is required")
	}
	if _, err := crypto.ParsePublicKey(tx.PublicKey); err != nil {
		return fmt.Errorf("public key: %w", err)
	}
	if len(tx.Signature) == 0 {
		return fmt.Errorf("account create signature is required")
	}
	signBytes, err := crypto.AccountCreateSignBytes(tx)
	if err != nil {
		return fmt.Errorf("canonical account create bytes: %w", err)
	}
	if !verifyAnyValidator(state, signBytes, tx.Signature) {
		return fmt.Errorf("invalid account create signature")
	}
	return nil
}

func verifyAnyValidator(state *State, message, signature []byte) bool {
	for _, pub := range state.ValidatorPubKeys {
		if crypto.Verify(pub, message, signature) {
			return true
		}
	}
	return false
}

// ApplyAccountCreate mutates state with a validated account create transaction.
func ApplyAccountCreate(tx domain.AccountCreateTransaction, state *State) error {
	if err := ValidateAccountCreate(tx, state); err != nil {
		return err
	}
	pub, err := crypto.ParsePublicKey(tx.PublicKey)
	if err != nil {
		return err
	}
	state.SetAccount(domain.Account{
		ID:      tx.Account.ID,
		Type:    tx.Account.Type,
		Balance: 0,
	})
	state.SetAccountPubKey(string(tx.Account.ID), pub)
	return nil
}

func ResolveCloseDestination(tx domain.AccountCloseTransaction, state *State) domain.AccountID {
	if tx.Destination != "" {
		return tx.Destination
	}
	return "federal_treasury"
}

func ValidateAccountClose(tx domain.AccountCloseTransaction, state *State) error {
	if tx.ID == "" {
		return fmt.Errorf("account close id is required")
	}
	if tx.AccountID == "" {
		return fmt.Errorf("account id is required")
	}
	acct, ok := state.Accounts[tx.AccountID]
	if !ok {
		return fmt.Errorf("account %q not found", tx.AccountID)
	}
	switch acct.Type {
	case domain.AccountTypePerson, domain.AccountTypeMerchant:
		// ok
	default:
		return fmt.Errorf("account type %q cannot be closed", acct.Type)
	}
	if tx.PublicKey == "" {
		return fmt.Errorf("public key is required")
	}
	pubFromTx, err := crypto.ParsePublicKey(tx.PublicKey)
	if err != nil {
		return fmt.Errorf("public key: %w", err)
	}
	acctPub, ok := state.AccountPubKey(string(tx.AccountID))
	if !ok {
		return fmt.Errorf("account public key not found for %q", tx.AccountID)
	}
	if string(pubFromTx) != string(acctPub) {
		return fmt.Errorf("public key does not match account %q", tx.AccountID)
	}

	if acct.Type == domain.AccountTypeMerchant {
		if acct.Balance != 0 {
			return fmt.Errorf("merchant account %q balance must be 0 to close (have %d)", tx.AccountID, acct.Balance)
		}
		if tx.Destination != "" {
			return fmt.Errorf("merchant close cannot specify destination")
		}
	} else {
		dest := ResolveCloseDestination(tx, state)
		if dest == tx.AccountID {
			return fmt.Errorf("destination cannot be the account being closed")
		}
		if _, ok := state.Accounts[dest]; !ok {
			return fmt.Errorf("destination account %q not found", dest)
		}
	}

	if len(tx.Signature) == 0 {
		return fmt.Errorf("account close signature is required")
	}
	signBytes, err := crypto.AccountCloseSignBytes(tx)
	if err != nil {
		return fmt.Errorf("canonical account close bytes: %w", err)
	}

	switch acct.Type {
	case domain.AccountTypePerson:
		if !verifyAnyValidator(state, signBytes, tx.Signature) {
			return fmt.Errorf("invalid account close signature")
		}
	case domain.AccountTypeMerchant:
		if !crypto.Verify(acctPub, signBytes, tx.Signature) {
			return fmt.Errorf("invalid account close signature")
		}
	}
	return nil
}

// ApplyAccountClose mutates state with a validated account close transaction.
func ApplyAccountClose(tx domain.AccountCloseTransaction, state *State) error {
	if err := ValidateAccountClose(tx, state); err != nil {
		return err
	}
	acct := state.Accounts[tx.AccountID]
	if acct.Type == domain.AccountTypePerson && acct.Balance > 0 {
		dest := ResolveCloseDestination(tx, state)
		destAcct := state.Accounts[dest]
		destAcct.Balance += acct.Balance
		state.Accounts[dest] = destAcct
	}
	delete(state.Accounts, tx.AccountID)
	state.RemoveAccountPubKey(string(tx.AccountID))
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
	for _, create := range block.AccountCreates {
		if err := ApplyAccountCreate(create, nextState); err != nil {
			return nil, fmt.Errorf("account create %s: %w", create.ID, err)
		}
	}
	for _, tx := range block.Transactions {
		if err := ApplyTransaction(tx, nextState); err != nil {
			return nil, fmt.Errorf("tx %s: %w", tx.ID, err)
		}
	}
	for _, closeTx := range block.AccountCloses {
		if err := ApplyAccountClose(closeTx, nextState); err != nil {
			return nil, fmt.Errorf("account close %s: %w", closeTx.ID, err)
		}
	}

	return nextState, nil
}
