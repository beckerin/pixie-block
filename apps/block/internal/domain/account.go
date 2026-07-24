package domain

type AccountID string

type Account struct {
	ID       AccountID `json:"id"`
	Balance  int64     `json:"balance"`
	Currency string    `json:"currency"`
}
