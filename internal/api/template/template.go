package template

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"strings"

	"github.com/beckerin/pixie-block/internal/domain"
)

//go:embed *.html
var files embed.FS

var moneyFuncs = htmltemplate.FuncMap{
	"money": formatMoney,
}

var (
	chainInfoTmpl = htmltemplate.Must(
		htmltemplate.New("chain_info.html").ParseFS(files, "chain_info.html"),
	)
	balanceTmpl = htmltemplate.Must(
		htmltemplate.New("balance.html").Funcs(moneyFuncs).ParseFS(files, "balance.html"),
	)
	viewerPanelTmpl = htmltemplate.Must(
		htmltemplate.New("viewer_panel.html").Funcs(moneyFuncs).ParseFS(files, "viewer_panel.html"),
	)
)

// ViewerPanelData drives the authenticated actions panel.
type ViewerPanelData struct {
	Anonymous   bool
	Audit       bool
	AccountID   string
	AccountType domain.AccountType
}

// BalanceData drives the SSE Balance event.
type BalanceData struct {
	Anonymous bool
	Audit     bool
	Account   *domain.Account
	Accounts  []domain.Account
}

func ChainInfo(info domain.ChainInfo) string {
	var buf bytes.Buffer
	if err := chainInfoTmpl.ExecuteTemplate(&buf, "chain_info.html", info); err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

func ViewerPanel(data ViewerPanelData) string {
	if data.Anonymous {
		return `<!-- anonymous -->`
	}
	var buf bytes.Buffer
	if err := viewerPanelTmpl.ExecuteTemplate(&buf, "viewer_panel.html", data); err != nil {
		return `<p class="text-ember font-mono text-sm">erro ao carregar painel</p>`
	}
	return strings.TrimSpace(buf.String())
}

func Balance(data BalanceData) string {
	if data.Anonymous {
		return `<!-- anonymous -->`
	}
	var buf bytes.Buffer
	if err := balanceTmpl.ExecuteTemplate(&buf, "balance.html", data); err != nil {
		return `<p class="text-ember font-mono text-sm">erro ao carregar saldo</p>`
	}
	return strings.TrimSpace(buf.String())
}

func BlockJSON(block domain.Block) string {
	return AnyJSON(block)
}

func AnyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "erro ao serializar bloco"
	}
	return string(b)
}

func PreviousBlock(latestHeight int64, previous domain.Block, found bool) string {
	if latestHeight <= 0 {
		return "nenhum bloco anterior"
	}
	if !found {
		return "bloco anterior não encontrado"
	}
	return BlockJSON(previous)
}

func PreviousBlockText(latestHeight int64, found bool, presentedJSON string) string {
	if latestHeight <= 0 {
		return "nenhum bloco anterior"
	}
	if !found {
		return "bloco anterior não encontrado"
	}
	return presentedJSON
}

func formatMoney(balance int64) string {
	sign := ""
	if balance < 0 {
		sign = "-"
		balance = -balance
	}
	whole := balance / 100
	frac := balance % 100
	return fmt.Sprintf("%sR$ %d,%02d", sign, whole, frac)
}
