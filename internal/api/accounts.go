package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/ledger"
)

type createAccountRequest struct {
	ID   string             `json:"id"`
	Type domain.AccountType `json:"type"`
}

type createAccountResponse struct {
	Account    domain.Account `json:"account"`
	PublicKey  string         `json:"public_key"`
	PrivateKey string         `json:"private_key"`
	TxID       string         `json:"tx_id"`
}

type closeAccountRequest struct {
	Destination string `json:"destination"`
}

type closeAccountResponse struct {
	TxID        string           `json:"tx_id"`
	AccountID   domain.AccountID `json:"account_id"`
	Destination domain.AccountID `json:"destination,omitempty"`
	Transferred int64            `json:"transferred"`
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.listAccounts())
	case http.MethodPost:
		s.handleCreateAccount(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if !s.canCreateAccounts || s.validatorPrivB64 == "" {
		writeError(w, http.StatusForbidden, "account creation requires a validator node")
		return
	}

	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate keypair: "+err.Error())
		return
	}

	tx := domain.AccountCreateTransaction{
		ID:        newTxID(),
		Timestamp: time.Now().UTC(),
		Account: domain.Account{
			ID:      domain.AccountID(req.ID),
			Type:    req.Type,
			Balance: 0,
		},
		PublicKey: crypto.PublicKeyBase64(pub),
	}

	validatorPriv, err := crypto.ParsePrivateKey(s.validatorPrivB64)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "validator key: "+err.Error())
		return
	}
	signBytes, err := crypto.AccountCreateSignBytes(tx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tx.Signature = crypto.Sign(validatorPriv, signBytes)

	if err := s.chain.ValidateAccountCreate(tx); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if s.createPool != nil {
		if err := s.createPool.TryAdd(tx, nil); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if s.submitCreates != nil {
		if err := s.submitCreates.SubmitAccountCreate(tx); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	privB64 := crypto.PrivateKeyBase64(priv)
	if err := s.appendKeystore(req.ID, privB64); err != nil {
		writeError(w, http.StatusInternalServerError, "persist keystore: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createAccountResponse{
		Account:    tx.Account,
		PublicKey:  tx.PublicKey,
		PrivateKey: privB64,
		TxID:       tx.ID,
	})
}

func (s *Server) appendKeystore(accountID, privB64 string) error {
	s.keystoreMu.Lock()
	defer s.keystoreMu.Unlock()

	s.keystore.AppendEntry(config.KeystoreEntry{
		AccountID:  accountID,
		PrivateKey: privB64,
	})
	if s.keystorePath == "" {
		return nil
	}
	return config.SaveKeystore(s.keystorePath, *s.keystore)
}

func (s *Server) removeKeystore(accountID string) error {
	s.keystoreMu.Lock()
	defer s.keystoreMu.Unlock()

	s.keystore.RemoveEntry(accountID)
	if s.keystorePath == "" {
		return nil
	}
	return config.SaveKeystore(s.keystorePath, *s.keystore)
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/accounts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch r.Method {
	case http.MethodDelete:
		if len(parts) != 1 || parts[0] == "" {
			writeError(w, http.StatusBadRequest, "use DELETE /v1/accounts/{id}")
			return
		}
		s.handleCloseAccount(w, r, parts[0])
		return
	case http.MethodGet:
		if len(parts) < 2 || parts[1] != "balance" {
			writeError(w, http.StatusBadRequest, "use /v1/accounts/{id}/balance")
			return
		}
		state := s.chain.State()
		acct, ok := state.Accounts[domain.AccountID(parts[0])]
		if !ok {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeJSON(w, http.StatusOK, acct)
		return
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleCloseAccount(w http.ResponseWriter, r *http.Request, accountID string) {
	state := s.chain.State()
	acct, ok := state.Accounts[domain.AccountID(accountID)]
	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	var req closeAccountRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	pub, ok := state.AccountPubKey(accountID)
	if !ok {
		writeError(w, http.StatusBadRequest, "account public key not found")
		return
	}

	tx := domain.AccountCloseTransaction{
		ID:        newTxID(),
		Timestamp: time.Now().UTC(),
		AccountID: domain.AccountID(accountID),
		PublicKey: crypto.PublicKeyBase64(pub),
	}

	var signPriv ed25519.PrivateKey
	switch acct.Type {
	case domain.AccountTypePerson:
		if !s.canCreateAccounts || s.validatorPrivB64 == "" {
			writeError(w, http.StatusForbidden, "closing a person account requires a validator node")
			return
		}
		if req.Destination != "" {
			tx.Destination = domain.AccountID(req.Destination)
		}
		priv, err := crypto.ParsePrivateKey(s.validatorPrivB64)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "validator key: "+err.Error())
			return
		}
		signPriv = priv
	case domain.AccountTypeMerchant:
		pkey := r.Header.Get("X-Private-Key")
		if pkey == "" {
			pkey = r.URL.Query().Get("pkey")
		}
		pkey = normalizePKey(pkey)
		if pkey == "" {
			writeError(w, http.StatusForbidden, "closing a merchant account requires the account private key")
			return
		}
		viewer := s.resolveViewerPKey(pkey)
		if viewer.AccountID != accountID && viewer.PublicKey != tx.PublicKey {
			writeError(w, http.StatusForbidden, "private key does not match merchant account")
			return
		}
		if req.Destination != "" {
			writeError(w, http.StatusBadRequest, "merchant close cannot specify destination")
			return
		}
		priv, err := crypto.ParsePrivateKey(pkey)
		if err != nil {
			writeError(w, http.StatusForbidden, "invalid private key")
			return
		}
		signPriv = priv
	default:
		writeError(w, http.StatusBadRequest, "account type cannot be closed")
		return
	}

	signBytes, err := crypto.AccountCloseSignBytes(tx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tx.Signature = crypto.Sign(signPriv, signBytes)

	if err := s.chain.ValidateAccountClose(tx); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	transferred := int64(0)
	destination := domain.AccountID("")
	if acct.Type == domain.AccountTypePerson {
		destination = ledger.ResolveCloseDestination(tx, state)
		transferred = acct.Balance
	}

	if s.closePool != nil {
		if err := s.closePool.TryAdd(tx, nil); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if s.submitCloses != nil {
		if err := s.submitCloses.SubmitAccountClose(tx); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := s.removeKeystore(accountID); err != nil {
		writeError(w, http.StatusInternalServerError, "persist keystore: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, closeAccountResponse{
		TxID:        tx.ID,
		AccountID:   tx.AccountID,
		Destination: destination,
		Transferred: transferred,
	})
}

func (s *Server) listAccounts() []domain.Account {
	state := s.chain.State()
	accounts := make([]domain.Account, 0, len(state.Accounts))
	for _, acct := range state.Accounts {
		accounts = append(accounts, acct)
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].ID < accounts[j].ID
	})
	return accounts
}
