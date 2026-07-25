package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.jetify.com/sse"

	"github.com/beckerin/pixie-block/internal/api/template"
	"github.com/beckerin/pixie-block/internal/domain"
)

func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	viewer := s.viewerFromRequest(r)

	conn, err := sse.Upgrade(r.Context(), w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	send := func(event string, id string, data string) error {
		return conn.SendEvent(r.Context(), &sse.Event{
			ID:    id,
			Event: event,
			Data:  sse.Raw(data),
			Split: strings.ContainsAny(data, "\r\n"),
		})
	}

	pushIfChanged := func(lastHeight *int64, initialized *bool) error {
		height := s.chain.Height()
		if *initialized && height == *lastHeight {
			return nil
		}
		*initialized = true
		*lastHeight = height
		id := fmt.Sprintf("height-%d", height)

		if err := send("ChainInfo", id, template.ChainInfo(s.chain.ChainInfo())); err != nil {
			return err
		}

		latest := s.chain.LatestBlock()
		if err := send("LatestBlock", id, template.AnyJSON(PresentBlock(latest, viewer, s.accountPubKeyB64))); err != nil {
			return err
		}

		previous, found := s.chain.GetBlock(latest.Height - 1)
		prevPayload := ""
		if latest.Height > 0 && found {
			prevPayload = template.AnyJSON(PresentBlock(previous, viewer, s.accountPubKeyB64))
		}
		if err := send("PreviousBlock", id, template.PreviousBlockText(latest.Height, found, prevPayload)); err != nil {
			return err
		}

		if err := send("ViewerPanel", id, s.viewerPanelHTML(viewer)); err != nil {
			return err
		}
		return send("Balance", id, s.balanceHTML(viewer))
	}

	var (
		lastHeight  int64
		initialized bool
	)
	if err := pushIfChanged(&lastHeight, &initialized); err != nil {
		return
	}

	for {
		select {
		case <-ticker.C:
			if err := pushIfChanged(&lastHeight, &initialized); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) viewerPanelHTML(viewer Viewer) string {
	if viewer.Audit {
		return template.ViewerPanel(template.ViewerPanelData{Audit: true})
	}
	if viewer.AccountID == "" {
		return template.ViewerPanel(template.ViewerPanelData{Anonymous: true})
	}
	acctType := domain.AccountType("")
	if acct, ok := s.chain.State().Accounts[domain.AccountID(viewer.AccountID)]; ok {
		acctType = acct.Type
	}
	return template.ViewerPanel(template.ViewerPanelData{
		AccountID:   viewer.AccountID,
		AccountType: acctType,
	})
}

func (s *Server) balanceHTML(viewer Viewer) string {
	state := s.chain.State()
	if viewer.Audit {
		accounts := make([]domain.Account, 0, len(state.Accounts))
		for _, acct := range state.Accounts {
			accounts = append(accounts, acct)
		}
		sort.Slice(accounts, func(i, j int) bool {
			return accounts[i].ID < accounts[j].ID
		})
		return template.Balance(template.BalanceData{Audit: true, Accounts: accounts})
	}
	if viewer.AccountID == "" {
		return template.Balance(template.BalanceData{Anonymous: true})
	}
	acct, ok := state.Accounts[domain.AccountID(viewer.AccountID)]
	if !ok {
		return template.Balance(template.BalanceData{Anonymous: true})
	}
	return template.Balance(template.BalanceData{Account: &acct})
}
