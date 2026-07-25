package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
)

// Viewer describes who is requesting a TX/block view.
type Viewer struct {
	AccountID string
	Audit     bool
}

// PublicTx is the redacted wire form of a payment transaction.
type PublicTx struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Payer     string    `json:"payer"`
	Payee     string    `json:"payee"`
	Currency  string    `json:"currency"`
	Items     []string  `json:"items"`
	Signature string    `json:"signature"`
}

// PublicBlock is a block whose transactions may be redacted per-viewer.
type PublicBlock struct {
	Height         int64                             `json:"height"`
	Timestamp      time.Time                         `json:"timestamp"`
	Transactions   []any                             `json:"transactions"`
	AccountCreates []domain.AccountCreateTransaction `json:"account_creates,omitempty"`
	PreviousHash   []byte                            `json:"previous_hash"`
	Hash           []byte                            `json:"hash"`
	Validator      string                            `json:"validator"`
	Signature      []byte                            `json:"signature,omitempty"`
}

// ResolveViewer maps a private key query value to an account viewer or validator auditor.
func ResolveViewer(keystore config.Keystore, validatorPrivB64, pkey string) Viewer {
	pkey = normalizePKey(pkey)
	if pkey == "" {
		return Viewer{}
	}
	if config.SamePrivateKey(pkey, validatorPrivB64) {
		return Viewer{Audit: true}
	}
	if id, ok := keystore.AccountIDForPrivateKey(pkey); ok {
		return Viewer{AccountID: id}
	}
	return Viewer{}
}

// PresentTx returns the full domain TX or a PublicTx depending on the viewer.
func PresentTx(tx domain.PaymentTransaction, viewer Viewer, pubKeyB64 func(domain.AccountID) string) any {
	if viewer.Audit || viewer.AccountID == string(tx.Payer.ID) || viewer.AccountID == string(tx.Payee.ID) {
		return tx
	}
	items := make([]string, len(tx.Items))
	for i, item := range tx.Items {
		items[i] = opaqueToken("item:" + item.Description)
	}
	return PublicTx{
		ID:        tx.ID,
		Timestamp: tx.Timestamp,
		Payer:     pubKeyB64(tx.Payer.ID),
		Payee:     pubKeyB64(tx.Payee.ID),
		Currency:  tx.Currency,
		Items:     items,
		Signature: opaqueTokenBytes(tx.Signature),
	}
}

// PresentBlock applies PresentTx to every transaction in the block.
func PresentBlock(block domain.Block, viewer Viewer, pubKeyB64 func(domain.AccountID) string) PublicBlock {
	txs := make([]any, len(block.Transactions))
	for i, tx := range block.Transactions {
		txs[i] = PresentTx(tx, viewer, pubKeyB64)
	}
	return PublicBlock{
		Height:         block.Height,
		Timestamp:      block.Timestamp,
		Transactions:   txs,
		AccountCreates: block.AccountCreates,
		PreviousHash:   block.PreviousHash,
		Hash:           block.Hash,
		Validator:      block.Validator,
		Signature:      block.Signature,
	}
}

func opaqueToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func opaqueTokenBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *Server) resolveViewerPKey(pkey string) Viewer {
	s.keystoreMu.Lock()
	ks := *s.keystore
	s.keystoreMu.Unlock()
	return ResolveViewer(ks, s.validatorPrivB64, pkey)
}

func (s *Server) viewerFromRequest(r *http.Request) Viewer {
	pkey := r.Header.Get("X-Private-Key")
	if pkey == "" {
		pkey = r.URL.Query().Get("pkey")
	}
	return s.resolveViewerPKey(pkey)
}

// normalizePKey restores '+' that query parsers turn into spaces in base64 keys.
func normalizePKey(pkey string) string {
	if pkey == "" {
		return ""
	}
	return strings.ReplaceAll(pkey, " ", "+")
}

func (s *Server) accountPubKeyB64(id domain.AccountID) string {
	if pub, ok := s.chain.State().AccountPubKey(string(id)); ok {
		return crypto.PublicKeyBase64(pub)
	}
	s.keystoreMu.Lock()
	privB64, ok := s.keystore.PrivateKeyFor(string(id))
	s.keystoreMu.Unlock()
	if ok {
		priv, err := crypto.ParsePrivateKey(privB64)
		if err == nil {
			return crypto.PublicKeyBase64(priv.Public().(ed25519.PublicKey))
		}
	}
	return string(id)
}
