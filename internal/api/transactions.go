package api

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
)

type submitTxRequest struct {
	Payer    string            `json:"payer"`
	Payee    string            `json:"payee"`
	Currency string            `json:"currency"`
	Items    []domain.LineItem `json:"items"`
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var req submitTxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := s.buildTransaction(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.chain.ValidatePending(tx); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.mempool.Add(tx); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if s.submit != nil {
		if err := s.submit.SubmitTransaction(tx); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusCreated, tx)
}

func (s *Server) buildTransaction(req submitTxRequest) (domain.PaymentTransaction, error) {
	tx := domain.PaymentTransaction{
		ID:        newTxID(),
		Timestamp: time.Now().UTC(),
		Payer:     domain.AccountID(req.Payer),
		Payee:     domain.AccountID(req.Payee),
		Currency:  req.Currency,
		Items:     req.Items,
	}

	privB64, ok := s.keystore.PrivateKeyFor(req.Payer)
	if !ok {
		return domain.PaymentTransaction{}, fmt.Errorf("no signing key for payer %q", req.Payer)
	}

	priv, err := crypto.ParsePrivateKey(privB64)
	if err != nil {
		return domain.PaymentTransaction{}, err
	}

	signBytes, err := crypto.TransactionSignBytes(tx)
	if err != nil {
		return domain.PaymentTransaction{}, err
	}

	tx.Signature = crypto.Sign(ed25519.PrivateKey(priv), signBytes)
	return tx, nil
}

func (s *Server) handleTransactionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/v1/transactions/")
	if id == "" {
		writeError(w, http.StatusBadRequest, "transaction id required")
		return
	}

	tx, ok := s.chain.GetTransaction(id)
	if !ok {
		if tx, ok = s.mempool.Get(id); !ok {
			writeError(w, http.StatusNotFound, "transaction not found")
			return
		}
	}

	writeJSON(w, http.StatusOK, tx)
}

func newTxID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
