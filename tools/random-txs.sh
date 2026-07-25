#!/usr/bin/env bash
# Gera transações aleatórias contra a API do pixie-block.
# Usa saldos ao vivo de /v1/accounts e só gasta o que o pagador ainda tem
# (evita falha no bloco: ValidatePending não enxerga o mempool).
#
# Uso:
#   ./tools/random-txs.sh
#   COUNT=300 API_URL=http://127.0.0.1:80 ./tools/random-txs.sh
#   ./tools/random-txs.sh --count 250 --api http://127.0.0.1:80

set -euo pipefail

API_URL="${API_URL:-http://127.0.0.1:80}"
COUNT="${COUNT:-200}"
MAX_AMOUNT="${MAX_AMOUNT:-5000}"   # centavos (R$ 50,00)
MIN_AMOUNT="${MIN_AMOUNT:-50}"     # centavos (R$ 0,50)
BATCH_SIZE="${BATCH_SIZE:-80}"     # pausa entre lotes (mempool Peek=100)
BATCH_SLEEP="${BATCH_SLEEP:-6}"    # segundos (block_time ~= 5s)
DRY_RUN=0

DESCRIPTIONS=(
  "Café especial"
  "Assinatura mensal"
  "Consultoria técnica"
  "Material de escritório"
  "Frete interestadual"
  "Licença de software"
  "Manutenção preventiva"
  "Kit higiene"
  "Almoço executivo"
  "Peças de reposição"
  "Curso online"
  "Serviço de limpeza"
  "Embalagens"
  "Hospedagem cloud"
  "Transporte urbano"
)

# Combinações seguras: soma das alíquotas < 100%
TAX_OPTIONS=(
  '[]'
  '["IBS"]'
  '["CBS"]'
  '["IBS","CBS"]'
  '["FGTS"]'
  '["IRRF"]'
  '["IBS","FGTS"]'
)

usage() {
  cat <<EOF
Uso: $0 [opções]

Opções:
  -c, --count N       Número de transações (default: ${COUNT})
  -a, --api URL       Base da API (default: ${API_URL})
  -s, --sleep N       Pausa entre transações (default: ${BATCH_SLEEP})
  -m, --max-amount N  Valor máximo em centavos (default: ${MAX_AMOUNT})
      --min-amount N  Valor mínimo em centavos (default: ${MIN_AMOUNT})
      --dry-run       Só imprime o payload, não envia
  -h, --help          Ajuda
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -c|--count) COUNT="$2"; shift 2 ;;
    -a|--api) API_URL="$2"; shift 2 ;;
    -s|--sleep) BATCH_SLEEP="$2"; shift 2 ;;
    -m|--max-amount) MAX_AMOUNT="$2"; shift 2 ;;
    --min-amount) MIN_AMOUNT="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Opção desconhecida: $1" >&2; usage; exit 1 ;;
  esac
done

if ! command -v jq >/dev/null 2>&1; then
  echo "jq é necessário" >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl é necessário" >&2
  exit 1
fi

if ! curl -sf --max-time 3 "${API_URL}/health" >/dev/null; then
  echo "API indisponível em ${API_URL}" >&2
  exit 1
fi

if [[ "$COUNT" -lt 200 ]]; then
  echo "Aviso: COUNT=${COUNT} < 200; seguindo mesmo assim." >&2
fi

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

BALANCES_FILE="${WORKDIR}/balances.json"
PAYERS_FILE="${WORKDIR}/payers.txt"
ACCOUNTS_FILE="${WORKDIR}/accounts.txt"

echo "Buscando contas em ${API_URL}/v1/accounts ..."
curl -sf --max-time 10 "${API_URL}/v1/accounts" >"${WORKDIR}/accounts.json"

# person/merchant têm chave no keystore e podem pagar; destino idem
jq -r '
  .[]
  | select(.type == "person" or .type == "merchant")
  | select(.balance >= '"$MIN_AMOUNT"')
  | "\(.id)\t\(.balance)\t\(.type)"
' "${WORKDIR}/accounts.json" >"$BALANCES_FILE"

jq -r '
  .[]
  | select(.type == "person" or .type == "merchant")
  | "\(.id)\t\(.type)"
' "${WORKDIR}/accounts.json" >"$ACCOUNTS_FILE"

cut -f1 "$BALANCES_FILE" >"$PAYERS_FILE"

PAYER_COUNT="$(wc -l <"$PAYERS_FILE" | tr -d ' ')"
ACCT_COUNT="$(wc -l <"$ACCOUNTS_FILE" | tr -d ' ')"
PERSON_COUNT="$(awk -F'\t' '$3=="person"' "$BALANCES_FILE" | wc -l | tr -d ' ')"
MERCHANT_COUNT="$(awk -F'\t' '$3=="merchant"' "$BALANCES_FILE" | wc -l | tr -d ' ')"

if [[ "$PAYER_COUNT" -lt 2 ]]; then
  echo "Poucos pagadores com saldo (encontrados: ${PAYER_COUNT})" >&2
  echo "Confirme que /v1/accounts expõe o campo type (person|merchant|treasury)." >&2
  exit 1
fi

echo "Pagadores: ${PAYER_COUNT} (person=${PERSON_COUNT} merchant=${MERCHANT_COUNT}) | Destinos: ${ACCT_COUNT}"
echo "Gerando ${COUNT} transações (min=${MIN_AMOUNT} max=${MAX_AMOUNT} centavos)..."
echo

ok=0
fail=0
attempt=0
max_attempts=$((COUNT * 20))

get_balance() {
  local id="$1"
  awk -F'\t' -v id="$id" '$1==id {print $2; found=1; exit} END{if(!found) print 0}' "$BALANCES_FILE"
}

set_balance() {
  local id="$1"
  local bal="$2"
  local tmp="${WORKDIR}/balances.tmp"
  awk -F'\t' -v id="$id" -v bal="$bal" '
    BEGIN{OFS="\t"}
    $1==id {$2=bal; updated=1}
    {print}
    END{if(!updated) print id, bal, "person"}
  ' "$BALANCES_FILE" >"$tmp"
  mv "$tmp" "$BALANCES_FILE"
}

random_line() {
  local file="$1"
  local n
  n="$(wc -l <"$file" | tr -d ' ')"
  if [[ "$n" -le 0 ]]; then
    return 1
  fi
  local idx=$((RANDOM % n + 1))
  sed -n "${idx}p" "$file"
}

while [[ "$ok" -lt "$COUNT" && "$attempt" -lt "$max_attempts" ]]; do
  attempt=$((attempt + 1))

  payer="$(random_line "$PAYERS_FILE")"
  payee_line="$(random_line "$ACCOUNTS_FILE")"
  payee="$(printf '%s' "$payee_line" | cut -f1)"
  payee_type="$(printf '%s' "$payee_line" | cut -f2)"
  if [[ -z "$payer" || -z "$payee" || "$payer" == "$payee" ]]; then
    continue
  fi

  balance="$(get_balance "$payer")"
  if [[ "$balance" -lt "$MIN_AMOUNT" ]]; then
    continue
  fi

  cap="$MAX_AMOUNT"
  if [[ "$balance" -lt "$cap" ]]; then
    cap="$balance"
  fi
  # evita gastar o saldo inteiro de uma vez
  soft=$((balance / 4))
  if [[ "$soft" -ge "$MIN_AMOUNT" && "$soft" -lt "$cap" ]]; then
    cap="$soft"
  fi
  if [[ "$cap" -lt "$MIN_AMOUNT" ]]; then
    continue
  fi

  amount=$((MIN_AMOUNT + RANDOM % (cap - MIN_AMOUNT + 1)))
  desc="${DESCRIPTIONS[$((RANDOM % ${#DESCRIPTIONS[@]}))]}"
  # impostos só para recebedor merchant/treasury (regra da API)
  if [[ "$payee_type" == "merchant" || "$payee_type" == "treasury" ]]; then
    taxes="${TAX_OPTIONS[$((RANDOM % ${#TAX_OPTIONS[@]}))]}"
  else
    taxes='[]'
  fi

  payload="$(jq -nc \
    --arg payer "$payer" \
    --arg payee "$payee" \
    --arg desc "$desc" \
    --argjson amount "$amount" \
    --argjson taxes "$taxes" \
    '{
      payer: $payer,
      payee: $payee,
      currency: "BRL",
      items: [{
        description: $desc,
        amount: $amount,
        tax_codes: $taxes
      }]
    }')"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "[dry-run] $payload"
    set_balance "$payer" "$((balance - amount))"
    ok=$((ok + 1))
  else
    resp_file="${WORKDIR}/resp.json"
    http_code="$(curl -sS --max-time 10 \
      -o "$resp_file" -w '%{http_code}' \
      -X POST "${API_URL}/v1/transactions" \
      -H 'Content-Type: application/json' \
      -d "$payload" || true)"

    if [[ "$http_code" == "201" ]]; then
      tx_id="$(jq -r '.id // empty' "$resp_file")"
      set_balance "$payer" "$((balance - amount))"
      ok=$((ok + 1))
      printf '[%3d/%d] OK  %s → %s  %d centavos  taxes=%s  id=%s\n' \
        "$ok" "$COUNT" "$payer" "$payee" "$amount" "$taxes" "$tx_id"
    else
      fail=$((fail + 1))
      err="$(jq -r '.error // .message // .' "$resp_file" 2>/dev/null || cat "$resp_file")"
      printf '[fail] HTTP %s  %s → %s  %d  %s\n' \
        "$http_code" "$payer" "$payee" "$amount" "$err" >&2
    fi
  fi

  # deixa o validador incluir lotes (Peek até 100 por bloco)
  if [[ "$DRY_RUN" -eq 0 && "$ok" -gt 0 && $((ok % BATCH_SIZE)) -eq 0 && "$ok" -lt "$COUNT" ]]; then
    echo "... pausa ${BATCH_SLEEP}s para produção de bloco (ok=${ok})"
    sleep "$BATCH_SLEEP"
  fi
done

echo
echo "Concluído: ok=${ok} fail=${fail} tentativas=${attempt}"
if [[ "$ok" -lt "$COUNT" ]]; then
  echo "Não atingiu ${COUNT} transações (saldos insuficientes ou API rejeitando)." >&2
  exit 1
fi
