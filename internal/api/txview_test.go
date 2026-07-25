package api_test

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/api"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
)

func TestResolveViewerRestoresPlusInPKey(t *testing.T) {
	var raw string
	var priv ed25519.PrivateKey
	for range 32 {
		_, p, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		b64 := crypto.PrivateKeyBase64(p)
		if strings.Contains(b64, "+") {
			raw, priv = b64, p
			break
		}
	}
	if raw == "" {
		t.Fatal("could not generate key containing '+'")
	}
	_ = priv
	keystore := config.Keystore{Entries: []config.KeystoreEntry{
		{AccountID: "person_001", PrivateKey: raw},
	}}
	corrupted := strings.ReplaceAll(raw, "+", " ")
	viewer := api.ResolveViewer(keystore, "", corrupted)
	if viewer.AccountID != "person_001" {
		t.Fatalf("viewer=%+v, want person_001 (pkey '+' became spaces)", viewer)
	}
}

func TestPresentTxPrivacyRules(t *testing.T) {
	payerPub, payerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	payeePub, payeePriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, thirdPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, validatorPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	keystore := config.Keystore{Entries: []config.KeystoreEntry{
		{AccountID: "merchant_001", PrivateKey: crypto.PrivateKeyBase64(payerPriv)},
		{AccountID: "person_001", PrivateKey: crypto.PrivateKeyBase64(payeePriv)},
		{AccountID: "person_002", PrivateKey: crypto.PrivateKeyBase64(thirdPriv)},
	}}
	validatorPrivB64 := crypto.PrivateKeyBase64(validatorPriv)

	pubKeys := map[domain.AccountID]string{
		"merchant_001": crypto.PublicKeyBase64(payerPub),
		"person_001":   crypto.PublicKeyBase64(payeePub),
	}
	lookup := func(id domain.AccountID) string { return pubKeys[id] }

	tx := domain.PaymentTransaction{
		ID:        "tx-1",
		Timestamp: time.Unix(1, 0).UTC(),
		Payer:     domain.Account{ID: "merchant_001", Type: domain.AccountTypeMerchant, Balance: 100},
		Payee:     domain.Account{ID: "person_001", Type: domain.AccountTypePerson, Balance: 50},
		Items:     []domain.LineItem{{Description: "Licença de software", Amount: 8}},
		Signature: []byte{1, 2, 3, 4},
	}

	t.Run("anonymous_public", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, "")
		got := api.PresentTx(tx, viewer, lookup)
		pub, ok := got.(api.PublicTx)
		if !ok {
			t.Fatalf("got %T, want PublicTx", got)
		}
		if pub.Payer != pubKeys["merchant_001"] || pub.Payee != pubKeys["person_001"] {
			t.Fatalf("payer/payee not pubkeys: %+v", pub)
		}
		if len(pub.Items) != 1 || pub.Items[0] == "Licença de software" {
			t.Fatalf("items should be opaque tokens: %#v", pub.Items)
		}
	})

	t.Run("payer_full", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, crypto.PrivateKeyBase64(payerPriv))
		got := api.PresentTx(tx, viewer, lookup)
		full, ok := got.(domain.PaymentTransaction)
		if !ok {
			t.Fatalf("got %T, want PaymentTransaction", got)
		}
		if full.Payer.ID != "merchant_001" {
			t.Fatalf("unexpected payer: %+v", full.Payer)
		}
	})

	t.Run("payee_full", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, crypto.PrivateKeyBase64(payeePriv))
		got := api.PresentTx(tx, viewer, lookup)
		if _, ok := got.(domain.PaymentTransaction); !ok {
			t.Fatalf("got %T, want PaymentTransaction", got)
		}
	})

	t.Run("third_party_public", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, crypto.PrivateKeyBase64(thirdPriv))
		if viewer.AccountID != "person_002" {
			t.Fatalf("viewer account = %q", viewer.AccountID)
		}
		got := api.PresentTx(tx, viewer, lookup)
		if _, ok := got.(api.PublicTx); !ok {
			t.Fatalf("got %T, want PublicTx", got)
		}
	})

	t.Run("validator_audit_full", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, validatorPrivB64)
		if !viewer.Audit {
			t.Fatal("expected audit viewer")
		}
		got := api.PresentTx(tx, viewer, lookup)
		if _, ok := got.(domain.PaymentTransaction); !ok {
			t.Fatalf("got %T, want PaymentTransaction", got)
		}
	})

	t.Run("invalid_pkey_public", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, "not-a-key")
		got := api.PresentTx(tx, viewer, lookup)
		if _, ok := got.(api.PublicTx); !ok {
			t.Fatalf("got %T, want PublicTx", got)
		}
	})
}

func TestPresentBlockValidatorSeesAll(t *testing.T) {
	_, payerPriv, _ := ed25519.GenerateKey(nil)
	_, payeePriv, _ := ed25519.GenerateKey(nil)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	_, validatorPriv, _ := ed25519.GenerateKey(nil)

	keystore := config.Keystore{Entries: []config.KeystoreEntry{
		{AccountID: "a", PrivateKey: crypto.PrivateKeyBase64(payerPriv)},
		{AccountID: "b", PrivateKey: crypto.PrivateKeyBase64(payeePriv)},
		{AccountID: "c", PrivateKey: crypto.PrivateKeyBase64(otherPriv)},
	}}
	validatorPrivB64 := crypto.PrivateKeyBase64(validatorPriv)
	lookup := func(id domain.AccountID) string { return string(id) }

	block := domain.Block{
		Height: 1,
		Transactions: []domain.PaymentTransaction{
			{
				ID:    "tx-1",
				Payer: domain.Account{ID: "a"},
				Payee: domain.Account{ID: "b"},
				Items: []domain.LineItem{{Description: "x", Amount: 1}},
			},
			{
				ID:    "tx-2",
				Payer: domain.Account{ID: "c"},
				Payee: domain.Account{ID: "a"},
				Items: []domain.LineItem{{Description: "y", Amount: 2}},
			},
		},
	}

	viewer := api.ResolveViewer(keystore, validatorPrivB64, validatorPrivB64)
	presented := api.PresentBlock(block, viewer, lookup)
	if len(presented.Transactions) != 2 {
		t.Fatalf("len=%d", len(presented.Transactions))
	}
	for i, raw := range presented.Transactions {
		if _, ok := raw.(domain.PaymentTransaction); !ok {
			t.Fatalf("tx[%d] type %T, want full", i, raw)
		}
	}

	// Ensure JSON shape is stable for PublicBlock.
	anon := api.PresentBlock(block, api.Viewer{}, lookup)
	data, err := json.Marshal(anon)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	txs, ok := decoded["transactions"].([]any)
	if !ok || len(txs) != 2 {
		t.Fatalf("transactions decode: %#v", decoded["transactions"])
	}
	first, ok := txs[0].(map[string]any)
	if !ok {
		t.Fatalf("tx0: %#v", txs[0])
	}
	if _, ok := first["payer"].(string); !ok {
		t.Fatalf("anonymous payer should be string, got %#v", first["payer"])
	}
}

func TestPresentAccountCreatePrivacyRules(t *testing.T) {
	acctPub, acctPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, validatorPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	acctPubB64 := crypto.PublicKeyBase64(acctPub)
	acctPrivB64 := crypto.PrivateKeyBase64(acctPriv)
	validatorPrivB64 := crypto.PrivateKeyBase64(validatorPriv)

	keystore := config.Keystore{Entries: []config.KeystoreEntry{
		{AccountID: "person_005", PrivateKey: acctPrivB64},
		{AccountID: "person_other", PrivateKey: crypto.PrivateKeyBase64(otherPriv)},
	}}

	create := domain.AccountCreateTransaction{
		ID:        "create-1",
		Timestamp: time.Unix(1, 0).UTC(),
		Account: domain.Account{
			ID:      "person_005",
			Type:    domain.AccountTypePerson,
			Balance: 0,
		},
		PublicKey: acctPubB64,
		Signature: []byte{9, 8, 7, 6},
	}

	t.Run("anonymous_public", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, "")
		got := api.PresentAccountCreate(create, viewer)
		pub, ok := got.(api.PublicAccountCreate)
		if !ok {
			t.Fatalf("got %T, want PublicAccountCreate", got)
		}
		if pub.PublicKey != acctPubB64 {
			t.Fatalf("public_key=%q", pub.PublicKey)
		}
		if pub.Signature == "" || pub.Signature == string(create.Signature) {
			t.Fatalf("signature should be opaque token, got %q", pub.Signature)
		}
		data, _ := json.Marshal(pub)
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, ok := decoded["id"]; ok {
			t.Fatal("public form must omit id")
		}
		if _, ok := decoded["account"]; ok {
			t.Fatal("public form must omit account")
		}
	})

	t.Run("owner_via_keystore_full", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, acctPrivB64)
		got := api.PresentAccountCreate(create, viewer)
		full, ok := got.(domain.AccountCreateTransaction)
		if !ok {
			t.Fatalf("got %T, want AccountCreateTransaction", got)
		}
		if full.Account.ID != "person_005" {
			t.Fatalf("account=%+v", full.Account)
		}
	})

	t.Run("owner_via_pubkey_without_keystore", func(t *testing.T) {
		emptyKS := config.Keystore{}
		viewer := api.ResolveViewer(emptyKS, validatorPrivB64, acctPrivB64)
		if viewer.AccountID != "" {
			t.Fatalf("expected empty AccountID, got %q", viewer.AccountID)
		}
		if viewer.PublicKey != acctPubB64 {
			t.Fatalf("PublicKey=%q want %q", viewer.PublicKey, acctPubB64)
		}
		got := api.PresentAccountCreate(create, viewer)
		if _, ok := got.(domain.AccountCreateTransaction); !ok {
			t.Fatalf("got %T, want full create via pubkey match", got)
		}
	})

	t.Run("third_party_public", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, crypto.PrivateKeyBase64(otherPriv))
		got := api.PresentAccountCreate(create, viewer)
		if _, ok := got.(api.PublicAccountCreate); !ok {
			t.Fatalf("got %T, want PublicAccountCreate", got)
		}
	})

	t.Run("validator_audit_full", func(t *testing.T) {
		viewer := api.ResolveViewer(keystore, validatorPrivB64, validatorPrivB64)
		if !viewer.Audit {
			t.Fatal("expected audit viewer")
		}
		got := api.PresentAccountCreate(create, viewer)
		if _, ok := got.(domain.AccountCreateTransaction); !ok {
			t.Fatalf("got %T, want AccountCreateTransaction", got)
		}
	})
}

func TestPresentBlockAccountCreatesRedacted(t *testing.T) {
	acctPub, acctPriv, _ := ed25519.GenerateKey(nil)
	_, validatorPriv, _ := ed25519.GenerateKey(nil)
	validatorPrivB64 := crypto.PrivateKeyBase64(validatorPriv)
	lookup := func(id domain.AccountID) string { return string(id) }

	block := domain.Block{
		Height: 2,
		AccountCreates: []domain.AccountCreateTransaction{{
			ID:        "create-1",
			Timestamp: time.Unix(2, 0).UTC(),
			Account:   domain.Account{ID: "person_005", Type: domain.AccountTypePerson},
			PublicKey: crypto.PublicKeyBase64(acctPub),
			Signature: []byte{1, 2, 3},
		}},
	}

	anon := api.PresentBlock(block, api.Viewer{}, lookup)
	if len(anon.AccountCreates) != 1 {
		t.Fatalf("len=%d", len(anon.AccountCreates))
	}
	if _, ok := anon.AccountCreates[0].(api.PublicAccountCreate); !ok {
		t.Fatalf("got %T, want PublicAccountCreate", anon.AccountCreates[0])
	}

	owner := api.ResolveViewer(config.Keystore{}, validatorPrivB64, crypto.PrivateKeyBase64(acctPriv))
	owned := api.PresentBlock(block, owner, lookup)
	if _, ok := owned.AccountCreates[0].(domain.AccountCreateTransaction); !ok {
		t.Fatalf("got %T, want full create", owned.AccountCreates[0])
	}
}
