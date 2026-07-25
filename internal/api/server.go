package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/chain"
	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/mempool"
)

type TxSubmitter interface {
	SubmitTransaction(tx domain.PaymentTransaction) error
}

type AccountCreateSubmitter interface {
	SubmitAccountCreate(tx domain.AccountCreateTransaction) error
}

type Server struct {
	chain             *chain.Blockchain
	mempool           *mempool.Pool
	createPool        *mempool.AccountCreatePool
	keystore          *config.Keystore
	keystoreMu        sync.Mutex
	keystorePath      string
	validatorPrivB64  string
	canCreateAccounts bool
	submit            TxSubmitter
	submitCreates     AccountCreateSubmitter
}

func NewServer(
	bc *chain.Blockchain,
	pool *mempool.Pool,
	createPool *mempool.AccountCreatePool,
	keystore *config.Keystore,
	keystorePath string,
	validatorPrivB64 string,
	canCreateAccounts bool,
	submitter TxSubmitter,
	createSubmitter AccountCreateSubmitter,
) *Server {
	return &Server{
		chain:             bc,
		mempool:           pool,
		createPool:        createPool,
		keystore:          keystore,
		keystorePath:      keystorePath,
		validatorPrivB64:  validatorPrivB64,
		canCreateAccounts: canCreateAccounts,
		submit:            submitter,
		submitCreates:     createSubmitter,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/chain", s.handleChain)
	mux.HandleFunc("/v1/transactions", s.handleTransactions)
	mux.HandleFunc("/v1/transactions/", s.handleTransactionByID)
	mux.HandleFunc("/v1/blocks/latest", s.handleLatestBlock)
	mux.HandleFunc("/v1/blocks/previous", s.handlePreviousBlock)
	mux.HandleFunc("/v1/blocks/", s.handleBlockByHeight)
	mux.HandleFunc("/v1/accounts", s.handleAccounts)
	mux.HandleFunc("/v1/accounts/", s.handleAccount)
	mux.Handle("/assets/", s.handleAssets())
	mux.Handle("/", s.handleStatic())
	return loggingMiddleware(mux)
}

func (s *Server) handleStatic() http.HandlerFunc {
	fs := http.FileServer(http.Dir("dist"))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			http.NotFound(w, r)
			return
		}
		fs.ServeHTTP(w, r)
	}
}

func (s *Server) handleAssets() http.Handler {
	return http.StripPrefix("/assets/", http.FileServer(http.Dir("dist/assets")))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start
	})
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	server := http.Server{
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
