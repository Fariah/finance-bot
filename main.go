package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"finance-bot/llm"
	"finance-bot/monobank"
	"finance-bot/storage"
	"finance-bot/telegram"
)

// App — головний оркестратор додатку
type App struct {
	storage     *storage.Storage
	mono        *monobank.Client
	tg          *telegram.Client
	llm         *llm.Client
	monoAccount string
}

// NewApp ініціалізує та повертає новий екземпляр App
func NewApp(dbPath, monoToken, monoAccount, tgToken, tgChatID, geminiKey string) (*App, error) {
	store, err := storage.NewStorage(dbPath)
	if err != nil {
		return nil, fmt.Errorf("помилка ініціалізації сховища: %w", err)
	}

	return &App{
		storage:     store,
		mono:        monobank.NewClient(monoToken),
		tg:          telegram.NewClient(tgToken, tgChatID),
		llm:         llm.NewClient(geminiKey),
		monoAccount: monoAccount,
	}, nil
}

// Sync завантажує транзакції, зберігає їх та надсилає звіт
func (app *App) Sync(days int) error {
	log.Println("Запуск синхронізації транзакцій...")

	now := time.Now()
	to := now.Unix()

	// Намагаємось дізнатися час останньої транзакції в БД
	latestTime, err := app.storage.GetLatestTransactionTimestamp()
	if err != nil {
		return fmt.Errorf("помилка отримання останнього таймстемпу: %w", err)
	}

	var from int64
	if latestTime > 0 {
		// Починаємо з наступної секунди після останньої транзакції
		from = latestTime + 1
	} else {
		// Якщо база порожня, завантажуємо дані за останні N днів
		from = now.AddDate(0, 0, -days).Unix()
	}

	// Ліміт Monobank: не більше 31 дня за один запит
	thirtyOneDaysAgo := now.AddDate(0, 0, -31).Unix()
	if from < thirtyOneDaysAgo {
		from = thirtyOneDaysAgo
	}

	// Якщо різниця часу занадто мала, пропускаємо
	if to-from < 10 {
		log.Println("Немає нових періодів для синхронізації (минуло мало часу з останнього запиту).")
		return nil
	}

	log.Printf("Запит транзакцій з Monobank за період з %s по %s",
		time.Unix(from, 0).Format("2006-01-02 15:04:05"),
		time.Unix(to, 0).Format("2006-01-02 15:04:05"),
	)

	items, err := app.mono.GetStatement(app.monoAccount, from, to)
	if err != nil {
		return fmt.Errorf("помилка отримання виписки з Monobank: %w", err)
	}

	if len(items) == 0 {
		log.Println("Нових транзакцій у вказаному періоді немає.")
		return nil
	}

	log.Printf("Отримано %d нових транзакцій. Зберігаємо у БД...", len(items))

	var txsToAnalyze []storage.Transaction
	for _, item := range items {
		txType := "expense"
		if item.Amount > 0 {
			txType = "income"
		}

		tx := storage.Transaction{
			ID:          item.ID,
			Amount:      item.Amount,
			Description: item.Description,
			MCC:         item.MCC,
			Timestamp:   item.Time,
			Type:        txType,
		}

		if err := app.storage.SaveTransaction(tx); err != nil {
			log.Printf("Помилка збереження транзакції %s у БД: %v", tx.ID, err)
			continue
		}
		txsToAnalyze = append(txsToAnalyze, tx)
	}

	if len(txsToAnalyze) == 0 {
		log.Println("Жодна нова транзакція не була збережена (можливо, всі вже існують).")
		return nil
	}

	log.Println("Формування аналітичного промпту для Gemini...")
	prompt := app.buildAnalysisPrompt(txsToAnalyze)

	log.Println("Запит аналізу у Gemini AI...")
	report, err := app.llm.Analyze(prompt)
	if err != nil {
		return fmt.Errorf("помилка генерації аналітики: %w", err)
	}

	log.Println("Надсилання звіту до Telegram...")
	if err := app.tg.SendMessage(report); err != nil {
		return fmt.Errorf("помилка надсилання в Telegram: %w", err)
	}

	log.Println("Синхронізацію успішно завершено!")
	return nil
}

// buildAnalysisPrompt формує структурований запит до штучного інтелекту
func (app *App) buildAnalysisPrompt(txs []storage.Transaction) string {
	var sb strings.Builder
	sb.WriteString("Ти — професійний фінансовий радник. Проаналізуй наступні транзакції користувача та дай короткі, корисні та дієві поради щодо оптимізації бюджету українською мовою. Форматуй відповідь у красивому Markdown для Telegram (використовуй жирний текст, списки, але уникай складного форматування).\n\nТранзакції:\n")

	for _, tx := range txs {
		amountFormatted := float64(tx.Amount) / 100.0
		sign := "-"
		if tx.Type == "income" {
			sign = "+"
		}
		timeStr := time.Unix(tx.Timestamp, 0).Format("02.01 15:04")
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s%.2f UAH (MCC: %d, %s)\n",
			timeStr, tx.Description, sign, math.Abs(amountFormatted), tx.MCC, tx.Type))
	}

	return sb.String()
}

// telegramUpdate описує структуру вхідного запиту від Telegram Webhook
type telegramUpdate struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
		MessageID int    `json:"message_id"`
		Text      string `json:"text,omitempty"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message,omitempty"`
}

// handleTelegramWebhook обробляє вхідні оновлення від Telegram бота (вебхуки)
func (app *App) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var update telegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Помилка декодування оновлення Telegram: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if update.Message != nil && update.Message.Text != "" {
		log.Printf("Отримано повідомлення від чату %d: %s", update.Message.Chat.ID, update.Message.Text)

		// Архітектурний заділ для команд:
		// cmd := update.Message.Text
		// if err := app.processCommand(update.Message.Chat.ID, cmd); err != nil {
		//     log.Printf("Помилка обробки команди: %v", err)
		// }
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// processCommand оброблятиме команди боту у майбутньому
func (app *App) processCommand(chatID int64, text string) error {
	// Приклад архітектурного розширення:
	// text = strings.TrimSpace(text)
	// switch text {
	// case "/sync":
	//     return app.Sync(7)
	// case "/stats":
	//     return app.tg.SendMessage("Статистика буде реалізована найближчим часом!")
	// default:
	//     return app.tg.SendMessage("Я розумію лише команди /sync та /stats")
	// }
	return nil
}

func main() {
	// Завантажуємо .env файл, якщо він є (для локальної розробки)
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "finance-bot.db"
	}

	monoToken := os.Getenv("MONOBANK_TOKEN")
	monoAccount := os.Getenv("MONOBANK_ACCOUNT_ID")
	tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	tgChatID := os.Getenv("TELEGRAM_CHAT_ID")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	// Перевірка наявності змінних середовища
	var missing []string
	if monoToken == "" {
		missing = append(missing, "MONOBANK_TOKEN")
	}
	if monoAccount == "" {
		missing = append(missing, "MONOBANK_ACCOUNT_ID")
	}
	if tgToken == "" {
		missing = append(missing, "TELEGRAM_BOT_TOKEN")
	}
	if tgChatID == "" {
		missing = append(missing, "TELEGRAM_CHAT_ID")
	}
	if geminiKey == "" {
		missing = append(missing, "GEMINI_API_KEY")
	}

	var app *App
	var initErr error

	if len(missing) == 0 {
		app, initErr = NewApp(dbPath, monoToken, monoAccount, tgToken, tgChatID, geminiKey)
		if initErr != nil {
			log.Fatalf("Помилка ініціалізації додатку: %v", initErr)
		}
		log.Println("Усі модулі фінансового бота успішно ініціалізовано!")
	} else {
		log.Printf("Попередження: Бот запущено в обмеженому демо-режимі (не всі змінні середовища присутні: %s)", strings.Join(missing, ", "))
		log.Println("Будь ласка, заповніть їх для повноцінної роботи.")
	}

	// Healthcheck для Fly.io (працює завжди, щоб Fly.io не перезапускав контейнер через невдалий деплой)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Ендпоінт для ручного запуску синхронізації
	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if app == nil {
			http.Error(w, "Помилка: Бот не ініціалізований. Перевірте конфігурацію.", http.StatusServiceUnavailable)
			return
		}

		days := 7 // За замовчуванням беремо виписку за останній тиждень, якщо БД порожня
		err := app.Sync(days)
		if err != nil {
			log.Printf("Помилка під час синхронізації: %v", err)
			http.Error(w, fmt.Sprintf("Synchronization failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Synchronization successful! Report sent to Telegram."))
	})

	// Ендпоінт для вебхуків Telegram (майбутній прийом команд)
	http.HandleFunc("/telegram/webhook", func(w http.ResponseWriter, r *http.Request) {
		if app == nil {
			http.Error(w, "Помилка: Бот не ініціалізований.", http.StatusServiceUnavailable)
			return
		}
		app.handleTelegramWebhook(w, r)
	})

	log.Printf("Сервер фінансового радника запущено на порту %s\n", port)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Помилка запуску сервера: %v", err)
	}
}
