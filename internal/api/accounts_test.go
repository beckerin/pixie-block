package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/api"
	"github.com/beckerin/pixie-block/internal/mempool"
)

func TestCreateAccountForbiddenWithoutValidator(t *testing.T) {
	ks := &config.Keystore{}
	server := api.NewServer(nil, mempool.New(), mempool.NewAccountCreatePool(), ks, "", "", false, nil, nil)

	body := bytes.NewBufferString(`{"id":"person_005","type":"person","currency":"BRL"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/accounts", body)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] == "" {
		t.Fatal("expected error message")
	}
}
