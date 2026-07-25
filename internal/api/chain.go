package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.jetify.com/sse"

	"github.com/beckerin/pixie-block/internal/api/template"
)

func (s *Server) handleChain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

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
		if err := send("LatestBlock", id, template.BlockJSON(s.chain.LatestBlock())); err != nil {
			return err
		}

		latest := s.chain.LatestBlock()
		previous, found := s.chain.GetBlock(latest.Height - 1)
		if err := send("PreviousBlock", id, template.PreviousBlock(latest.Height, previous, found)); err != nil {
			return err
		}
		return send("Accounts", id, template.Accounts(s.listAccounts()))
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
