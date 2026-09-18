package monobank

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const baseURL = "https://api.monobank.ua"

// Client for interacting with Monobank API
type Client struct {
	token      string
	httpClient *http.Client
}

// StatementItem describes one transaction from Monobank statement
type StatementItem struct {
	ID          string `json:"id"`
	Time        int64  `json:"time"`
	Description string `json:"description"`
	MCC         int    `json:"mcc"`
	Amount      int64  `json:"amount"` // In kopiykas. Negative is expense, positive is income
}

// NewClient creates a new client
func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetStatement retrieves a statement for the specified period
func (c *Client) GetStatement(account string, from, to int64) ([]StatementItem, error) {
	url := fmt.Sprintf("%s/personal/statement/%s/%d/%d", baseURL, account, from, to)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("X-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("monobank returned status: %d", resp.StatusCode)
	}

	var items []StatementItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("error decoding JSON: %w", err)
	}

	return items, nil
}