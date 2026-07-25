package api

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleLatestBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	viewer := s.viewerFromRequest(r)
	writeJSON(w, http.StatusOK, PresentBlock(s.chain.LatestBlock(), viewer, s.accountPubKeyB64))
}

func (s *Server) handlePreviousBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	latest := s.chain.LatestBlock()
	if latest.Height <= 0 {
		writeError(w, http.StatusNotFound, "no previous block")
		return
	}

	block, ok := s.chain.GetBlock(latest.Height - 1)
	if !ok {
		writeError(w, http.StatusNotFound, "previous block not found")
		return
	}

	viewer := s.viewerFromRequest(r)
	writeJSON(w, http.StatusOK, PresentBlock(block, viewer, s.accountPubKeyB64))
}

func (s *Server) handleBlockByHeight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/blocks/")
	switch path {
	case "latest":
		s.handleLatestBlock(w, r)
		return
	case "previous":
		s.handlePreviousBlock(w, r)
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

	viewer := s.viewerFromRequest(r)
	writeJSON(w, http.StatusOK, PresentBlock(block, viewer, s.accountPubKeyB64))
}
