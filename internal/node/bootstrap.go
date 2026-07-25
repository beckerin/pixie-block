package node

import (
	"crypto/ed25519"
	"fmt"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/ledger"
)

func BuildInitialState(genesis config.Genesis, keystore config.Keystore, taxes config.Taxes) (*ledger.State, error) {
	if err := taxes.Validate(genesis.AllowedTaxAccounts); err != nil {
		return nil, fmt.Errorf("taxes: %w", err)
	}

	allowed := make([]domain.AccountID, len(genesis.AllowedTaxAccounts))
	for i, id := range genesis.AllowedTaxAccounts {
		allowed[i] = domain.AccountID(id)
	}

	state := ledger.NewState(domain.AccountID(genesis.TaxTreasury), allowed, taxes)

	for _, acct := range genesis.Accounts {
		state.SetAccount(domain.Account{
			ID:       domain.AccountID(acct.ID),
			Type:     acct.Type,
			Balance:  acct.Balance,
			Currency: acct.Currency,
		})
	}

	for _, v := range genesis.Validators {
		pub, err := crypto.ParsePublicKey(v.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("validator %s: %w", v.ID, err)
		}
		state.AddValidator(v.ID, pub)
	}

	for _, entry := range keystore.Entries {
		priv, err := crypto.ParsePrivateKey(entry.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("keystore %s: %w", entry.AccountID, err)
		}
		pub := priv.Public().(ed25519.PublicKey)
		state.SetAccountPubKey(entry.AccountID, pub)
	}

	return state, nil
}

func HydrateState(state *ledger.State, genesis config.Genesis, keystore config.Keystore) error {
	for _, v := range genesis.Validators {
		pub, err := crypto.ParsePublicKey(v.PublicKey)
		if err != nil {
			return err
		}
		state.AddValidator(v.ID, pub)
	}

	for _, entry := range keystore.Entries {
		priv, err := crypto.ParsePrivateKey(entry.PrivateKey)
		if err != nil {
			return err
		}
		pub := priv.Public().(ed25519.PublicKey)
		state.SetAccountPubKey(entry.AccountID, pub)
	}

	return nil
}
