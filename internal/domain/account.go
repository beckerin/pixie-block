package domain

type AccountID string
type AccountType string

type Account struct {
	ID      AccountID   `json:"id"`
	Type    AccountType `json:"type"`
	Balance int64       `json:"balance"`
}

const (
	AccountTypePerson   AccountType = "person"   // Pessoa física
	AccountTypeMerchant AccountType = "merchant" // Pessoa jurídica
	AccountTypeTreasury AccountType = "treasury" // Órgão fiscal
)
