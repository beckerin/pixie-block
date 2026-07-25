package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
)

type createAccountRequest struct {
	ID       string             `json:"id"`
	Type     domain.AccountType `json:"type"`
	Currency string             `json:"currency"`
}

type createAccountResponse struct {
	Account    domain.Account `json:"account"`
	PublicKey  string         `json:"public_key"`
	PrivateKey string         `json:"private_key"`
	TxID       string         `json:"tx_id"`
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
	if req.Currency == "" {
		req.Currency = "BRL"
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
			ID:       domain.AccountID(req.ID),
			Type:     req.Type,
			Balance:  0,
			Currency: req.Currency,
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

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/accounts/")
	parts := strings.Split(path, "/")
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
