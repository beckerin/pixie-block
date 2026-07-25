package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/beckerin/pixie-block/internal/domain"
)

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	writeJSON(w, http.StatusOK, s.listAccounts())
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
