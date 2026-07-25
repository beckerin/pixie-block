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

var (
	chainInfoTmpl = htmltemplate.Must(
		htmltemplate.New("chain_info.html").ParseFS(files, "chain_info.html"),
	)
	accountsTmpl = htmltemplate.Must(
		htmltemplate.New("accounts.html").Funcs(htmltemplate.FuncMap{
			"money": formatMoney,
		}).ParseFS(files, "accounts.html"),
	)
)

func ChainInfo(info domain.ChainInfo) string {
	var buf bytes.Buffer
	if err := chainInfoTmpl.ExecuteTemplate(&buf, "chain_info.html", info); err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

func Accounts(accounts []domain.Account) string {
	var buf bytes.Buffer
	if err := accountsTmpl.ExecuteTemplate(&buf, "accounts.html", accounts); err != nil {
		return `<p class="text-ember font-mono text-sm">erro ao carregar contas</p>`
	}
	return strings.TrimSpace(buf.String())
}

func BlockJSON(block domain.Block) string {
	b, err := json.MarshalIndent(block, "", "  ")
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

func formatMoney(balance int64, currency string) string {
	if currency == "" {
		currency = "BRL"
	}
	sign := ""
	if balance < 0 {
		sign = "-"
		balance = -balance
	}
	whole := balance / 100
	frac := balance % 100
	if currency == "BRL" {
		return fmt.Sprintf("%sR$ %d,%02d", sign, whole, frac)
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, currency, whole, frac)
}
