package monobank

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const baseURL = "https://api.monobank.ua"

// Client для взаємодії з API Монобанку
type Client struct {
	token      string
	httpClient *http.Client
}

// StatementItem описує одну транзакцію з виписки Монобанку
type StatementItem struct {
	ID          string `json:"id"`
	Time        int64  `json:"time"`
	Description string `json:"description"`
	MCC         int    `json:"mcc"`
	Amount      int64  `json:"amount"` // В копійках. Від'ємне — витрата, додатне — дохід
}

// NewClient створює новий клієнт
func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetStatement отримує виписку за вказаний період
func (c *Client) GetStatement(account string, from, to int64) ([]StatementItem, error) {
	url := fmt.Sprintf("%s/personal/statement/%s/%d/%d", baseURL, account, from, to)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("помилка створення запиту: %w", err)
	}

	req.Header.Set("X-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("помилка виконання запиту: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("монобанк повернув статус: %d", resp.StatusCode)
	}

	var items []StatementItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("помилка декодування JSON: %w", err)
	}

	return items, nil
}