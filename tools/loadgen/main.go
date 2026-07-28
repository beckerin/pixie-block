package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type account struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Balance int64  `json:"balance"`
}

type lineItem struct {
	Description string   `json:"description"`
	Amount      int64    `json:"amount"`
	TaxCodes    []string `json:"tax_codes"`
}

type submitReq struct {
	Payer string     `json:"payer"`
	Payee string     `json:"payee"`
	Items []lineItem `json:"items"`
}

var descriptions = []string{
	"Café especial",
	"Assinatura mensal",
	"Consultoria técnica",
	"Material de escritório",
	"Frete interestadual",
	"Licença de software",
	"Manutenção preventiva",
	"Kit higiene",
	"Almoço executivo",
	"Peças de reposição",
}

var taxOptions = [][]string{
	{},
	{"IBS"},
	{"CBS"},
	{"IBS", "CBS"},
}

// Matches config/taxes.json — used to credit net-to-payee locally.
var taxRateBPS = map[string]int64{
	"IBS": 1700,
	"CBS": 1100,
}

func main() {
	var (
		apiURL     = flag.String("api", "http://127.0.0.1:80", "validator API base URL")
		n          = flag.Int("n", 5000, "number of successful admits to target")
		workers    = flag.Int("workers", 0, "parallel POST workers (0 = min(32, payers/2))")
		minAmount  = flag.Int64("min-amount", 1, "minimum amount in centavos")
		maxAmount  = flag.Int64("max-amount", 50, "maximum amount in centavos")
		timeoutSec = flag.Int("timeout", 10, "HTTP timeout seconds")
	)
	flag.Parse()

	client := &http.Client{
		Timeout: time.Duration(*timeoutSec) * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 256,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}

	accounts, err := fetchAccounts(client, *apiURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch accounts: %v\n", err)
		os.Exit(1)
	}

	balances := make(map[string]int64)
	var payers []string
	var payees []account
	for _, a := range accounts {
		if a.Type != "person" && a.Type != "merchant" {
			continue
		}
		payees = append(payees, a)
		balances[a.ID] = a.Balance
		if a.Balance >= *minAmount {
			payers = append(payers, a.ID)
		}
	}
	if len(payers) < 2 || len(payees) < 2 {
		fmt.Fprintf(os.Stderr, "need >=2 payers/payees with balance (payers=%d payees=%d)\n", len(payers), len(payees))
		os.Exit(1)
	}

	numWorkers := *workers
	if numWorkers < 1 {
		numWorkers = len(payers) / 2
		if numWorkers > 32 {
			numWorkers = 32
		}
		if numWorkers < 1 {
			numWorkers = 1
		}
	}
	if numWorkers > len(payers) {
		numWorkers = len(payers)
	}

	var (
		okCount   atomic.Int64
		failCount atomic.Int64
		done      atomic.Bool
		balMu     sync.Mutex
		reasonMu  sync.Mutex
		reasons   = make(map[string]int64)
	)
	target := int64(*n)

	fmt.Fprintf(os.Stderr, "loadgen: payers=%d payees=%d workers=%d target=%d\n",
		len(payers), len(payees), numWorkers, target)

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		// Shard payers so workers rarely contend on the same spender.
		myPayers := shardPayers(payers, w, numWorkers)
		go func(seed int64, myPayers []string) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed + time.Now().UnixNano()))
			idle := 0
			for !done.Load() {
				if okCount.Load() >= target {
					done.Store(true)
					return
				}
				payer, payee, amount, taxes, okPick := pickTx(
					&balMu, balances, myPayers, payees, *minAmount, *maxAmount, rng,
				)
				if !okPick {
					idle++
					failCount.Add(1)
					recordReason(&reasonMu, reasons, "no_local_balance")
					if idle > 64 {
						time.Sleep(2 * time.Millisecond)
						idle = 0
					}
					if failCount.Load() > target*50 {
						done.Store(true)
						return
					}
					continue
				}
				idle = 0
				req := submitReq{
					Payer: payer,
					Payee: payee.ID,
					Items: []lineItem{{
						Description: descriptions[rng.Intn(len(descriptions))],
						Amount:      amount,
						TaxCodes:    taxes,
					}},
				}
				code, errMsg := postTx(client, *apiURL, req)
				if code == http.StatusCreated {
					// Recirculate net-to-payee so large runs don't drain local liquidity.
					// Slightly optimistic vs confirmed state (credit before block apply).
					netAmt := amount - taxTotal(amount, taxes)
					balMu.Lock()
					balances[payee.ID] += netAmt
					balMu.Unlock()
					if okCount.Add(1) >= target {
						done.Store(true)
					}
					continue
				}
				balMu.Lock()
				balances[payer] += amount
				balMu.Unlock()
				failCount.Add(1)
				reason := errMsg
				if reason == "" {
					reason = fmt.Sprintf("http_%d", code)
				}
				recordReason(&reasonMu, reasons, reason)
			}
		}(int64(w), myPayers)
	}
	wg.Wait()
	elapsed := time.Since(start)

	ok := okCount.Load()
	fail := failCount.Load()
	rate := float64(0)
	if elapsed > 0 {
		rate = float64(ok) / elapsed.Seconds()
	}
	fmt.Printf("ok=%d fail=%d elapsed=%s admit_rate=%.1f tx/s workers=%d\n",
		ok, fail, elapsed.Round(time.Millisecond), rate, numWorkers)
	printTopReasons(reasons, 8)
	if ok < target {
		os.Exit(1)
	}
}

func shardPayers(payers []string, worker, workers int) []string {
	var out []string
	for i := worker; i < len(payers); i += workers {
		out = append(out, payers[i])
	}
	if len(out) == 0 {
		return payers
	}
	return out
}

func taxTotal(amount int64, taxes []string) int64 {
	var total int64
	for _, code := range taxes {
		total += taxRateBPS[code] * amount / 10000
	}
	return total
}

func normalizeReason(reason string) string {
	switch {
	case reason == "":
		return "unknown"
	case strings.HasPrefix(reason, "insufficient balance"):
		return "insufficient_balance"
	case strings.Contains(reason, "connection refused"), strings.Contains(reason, "connect: connection"):
		return "connection_error"
	case strings.Contains(reason, "Timeout"), strings.Contains(reason, "timeout"):
		return "timeout"
	default:
		return reason
	}
}

func recordReason(mu *sync.Mutex, reasons map[string]int64, reason string) {
	mu.Lock()
	reasons[normalizeReason(reason)]++
	mu.Unlock()
}

func printTopReasons(reasons map[string]int64, n int) {
	if len(reasons) == 0 {
		return
	}
	type kv struct {
		k string
		v int64
	}
	var items []kv
	for k, v := range reasons {
		items = append(items, kv{k, v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].v > items[j].v })
	if n > len(items) {
		n = len(items)
	}
	fmt.Println("top reject reasons:")
	for i := 0; i < n; i++ {
		fmt.Printf("  %d  %s\n", items[i].v, items[i].k)
	}
}

func fetchAccounts(client *http.Client, apiURL string) ([]account, error) {
	resp, err := client.Get(apiURL + "/v1/accounts")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var accounts []account
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

func pickTx(
	mu *sync.Mutex,
	balances map[string]int64,
	payers []string,
	payees []account,
	minAmount, maxAmount int64,
	rng *rand.Rand,
) (payer string, payee account, amount int64, taxes []string, ok bool) {
	mu.Lock()
	defer mu.Unlock()

	if len(payers) == 0 {
		return "", account{}, 0, nil, false
	}

	for attempt := 0; attempt < 32; attempt++ {
		payer = payers[rng.Intn(len(payers))]
		payee = payees[rng.Intn(len(payees))]
		if payer == payee.ID {
			continue
		}
		bal := balances[payer]
		if bal < minAmount {
			continue
		}
		capAmt := maxAmount
		if bal < capAmt {
			capAmt = bal
		}
		// Keep headroom for in-flight mempool + concurrent external clients.
		soft := bal / 8
		if soft >= minAmount && soft < capAmt {
			capAmt = soft
		}
		if capAmt < minAmount {
			continue
		}
		amount = minAmount + rng.Int63n(capAmt-minAmount+1)
		if payee.Type == "merchant" || payee.Type == "treasury" {
			taxes = taxOptions[rng.Intn(len(taxOptions))]
		} else {
			taxes = nil
		}
		balances[payer] -= amount
		return payer, payee, amount, taxes, true
	}
	return "", account{}, 0, nil, false
}

func postTx(client *http.Client, apiURL string, req submitReq) (int, string) {
	body, err := json.Marshal(req)
	if err != nil {
		return 0, err.Error()
	}
	resp, err := client.Post(apiURL+"/v1/transactions", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, ""
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	var errObj struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &errObj) == nil && errObj.Error != "" {
		return resp.StatusCode, errObj.Error
	}
	return resp.StatusCode, string(bytes.TrimSpace(raw))
}
