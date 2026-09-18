package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const apiURL = "https://api.telegram.org/bot%s/%s"

// Client for interacting with Telegram Bot API
type Client struct {
	botToken string
	chatID   string
	client   *http.Client
}

// sendMessagePayload describes the JSON structure for sending a message
type sendMessagePayload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// NewClient creates a new Telegram client
func NewClient(botToken, chatID string) *Client {
	return &Client{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 10 * time.Second, // Timeout to prevent hanging
		},
	}
}

// SendMessage sends a text message to the chat
func (c *Client) SendMessage(text string) error {
	url := fmt.Sprintf(apiURL, c.botToken, "sendMessage")

	payload := sendMessagePayload{
		ChatID:    c.chatID,
		Text:      text,
		ParseMode: "Markdown", // Allow LLM to use bold text or lists
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error forming JSON for Telegram: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("error creating request to Telegram: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status: %d", resp.StatusCode)
	}

	return nil
}