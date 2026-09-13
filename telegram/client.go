package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const apiURL = "https://api.telegram.org/bot%s/%s"

// Client для взаємодії з Telegram Bot API
type Client struct {
	botToken string
	chatID   string
	client   *http.Client
}

// sendMessagePayload описує структуру JSON для відправки повідомлення
type sendMessagePayload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// NewClient створює новий клієнт Telegram
func NewClient(botToken, chatID string) *Client {
	return &Client{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 10 * time.Second, // Таймаут, щоб не зависало
		},
	}
}

// SendMessage відправляє текстове повідомлення у ваш чат
func (c *Client) SendMessage(text string) error {
	url := fmt.Sprintf(apiURL, c.botToken, "sendMessage")

	payload := sendMessagePayload{
		ChatID:    c.chatID,
		Text:      text,
		ParseMode: "Markdown", // Дозволить LLM використовувати жирний текст чи списки
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("помилка формування JSON для Telegram: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("помилка створення запиту до Telegram: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("помилка відправки повідомлення: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API повернув статус: %d", resp.StatusCode)
	}

	return nil
}