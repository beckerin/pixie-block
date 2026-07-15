package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/solidk-tech/pixie-block/config"
	"github.com/solidk-tech/pixie-block/internal/chain"
	"github.com/solidk-tech/pixie-block/internal/crypto"
	"github.com/solidk-tech/pixie-block/internal/domain"
	"github.com/solidk-tech/pixie-block/internal/mempool"
)

type TxSubmitter interface {
	SubmitTransaction(tx domain.PaymentTransaction) error
}

type Server struct {
	chain    *chain.Blockchain
	mempool  *mempool.Pool
	keystore config.Keystore
	submit   TxSubmitter
}

func NewServer(bc *chain.Blockchain, pool *mempool.Pool, keystore config.Keystore, submitter TxSubmitter) *Server {
	return &Server{
		chain:    bc,
		mempool:  pool,
		keystore: keystore,
		submit:   submitter,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/chain", s.handleChain)
	mux.HandleFunc("/v1/transactions", s.handleTransactions)
	mux.HandleFunc("/v1/transactions/", s.handleTransactionByID)
	mux.HandleFunc("/v1/blocks/latest", s.handleLatestBlock)
	mux.HandleFunc("/v1/blocks/", s.handleBlockByHeight)
	mux.HandleFunc("/v1/accounts/", s.handleAccount)
	return loggingMiddleware(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.chain.ChainInfo())
}

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

func (s *Server) handleLatestBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.chain.LatestBlock())
}

func (s *Server) handleBlockByHeight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/blocks/")
	if path == "latest" {
		s.handleLatestBlock(w, r)
		return
	}

	height, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid block height")
		return
	}

	block, ok := s.chain.GetBlock(height)
	if !ok {
		writeError(w, http.StatusNotFound, "block not found")
		return
	}

	writeJSON(w, http.StatusOK, block)
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

func newTxID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start
	})
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	server := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	return server.ListenAndServe()
}
