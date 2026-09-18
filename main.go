package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"finance-bot/llm"
	"finance-bot/monobank"
	"finance-bot/storage"
	"finance-bot/telegram"
)

// App is the main app orchestrator
type App struct {
	storage     *storage.Storage
	mono        *monobank.Client
	tg          *telegram.Client
	llm         *llm.Client
	monoAccount string
}

// NewApp initializes and returns a new App instance
func NewApp(dbPath, monoToken, monoAccount, tgToken, tgChatID, geminiKey string) (*App, error) {
	store, err := storage.NewStorage(dbPath)
	if err != nil {
		return nil, fmt.Errorf("storage initialization error: %w", err)
	}

	return &App{
		storage:     store,
		mono:        monobank.NewClient(monoToken),
		tg:          telegram.NewClient(tgToken, tgChatID),
		llm:         llm.NewClient(geminiKey),
		monoAccount: monoAccount,
	}, nil
}

// Sync loads transactions, saves them and sends a report
func (app *App) Sync(days int) error {
	log.Println("Starting transaction synchronization...")

	now := time.Now()
	to := now.Unix()

	// Try to find the time of the last transaction in the database
	latestTime, err := app.storage.GetLatestTransactionTimestamp()
	if err != nil {
		return fmt.Errorf("error getting last timestamp: %w", err)
	}

	var from int64
	if latestTime > 0 {
		// Start from the next second after the last transaction
		from = latestTime + 1
	} else {
		// If the database is empty, load data for the last N days
		from = now.AddDate(0, 0, -days).Unix()
	}

	// Monobank limit: no more than 31 days per request
	thirtyOneDaysAgo := now.AddDate(0, 0, -31).Unix()
	if from < thirtyOneDaysAgo {
		from = thirtyOneDaysAgo
	}

	// If the time difference is too small, skip
	if to-from < 10 {
		log.Println("No new periods to synchronize (too little time since last request).")
		return nil
	}

	log.Printf("Requesting transactions from Monobank for period from %s to %s",
		time.Unix(from, 0).Format("2006-01-02 15:04:05"),
		time.Unix(to, 0).Format("2006-01-02 15:04:05"),
	)

	items, err := app.mono.GetStatement(app.monoAccount, from, to)
	if err != nil {
		return fmt.Errorf("error getting statement from Monobank: %w", err)
	}

	if len(items) == 0 {
		log.Println("No new transactions in the specified period.")
		return nil
	}

	log.Printf("Received %d new transactions. Saving to database...", len(items))

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
			log.Printf("Error saving transaction %s to database: %v", tx.ID, err)
			continue
		}
		txsToAnalyze = append(txsToAnalyze, tx)
	}

	if len(txsToAnalyze) == 0 {
		log.Println("No new transactions were saved (maybe they all already exist).")
		return nil
	}

	log.Println("Calculating budget status for prompt...")
	budgetStatus, err := app.CalculateBudgetStatus(now)
	if err != nil {
		log.Printf("Warning: failed to calculate budget: %v", err)
	}

	obligationsDue, err := app.storage.ObligationsDueThisMonth(now)
	if err != nil {
		log.Printf("Warning: failed to get obligations: %v", err)
	}

	pendingPlanned, err := app.storage.ListPlannedExpenses("pending")
	if err != nil {
		log.Printf("Warning: failed to get planned expenses: %v", err)
	}

	log.Println("Building analysis prompt for Gemini...")
	prompt := app.buildAnalysisPrompt(txsToAnalyze, budgetStatus, obligationsDue, pendingPlanned)

	log.Println("Requesting analysis from Gemini AI...")
	report, err := app.llm.Analyze(prompt)
	if err != nil {
		return fmt.Errorf("error generating analytics: %w", err)
	}

	log.Println("Sending report to Telegram...")
	if err := app.tg.SendMessage(report); err != nil {
		return fmt.Errorf("error sending to Telegram: %w", err)
	}

	if budgetStatus.HasIncome {
		if err := app.tg.SendMessage(app.formatBudgetStatus(budgetStatus)); err != nil {
			log.Printf("Error sending budget status: %v", err)
		}
	}

	log.Println("Synchronization completed successfully!")
	return nil
}

// BudgetStatus describes the calculated budget status for the current month
type BudgetStatus struct {
	HasIncome            bool
	MonthlyIncomeCents   int64
	ObligationsCents     int64
	ApprovedPlannedCents int64
	SpentCents           int64
	RemainingCents       int64
	DaysLeftInMonth      int
	SuggestedDailyLimit  int64
}

// CalculateBudgetStatus calculates how much money remains for the month considering
// income, mandatory payments for this month and already spent
func (app *App) CalculateBudgetStatus(now time.Time) (BudgetStatus, error) {
	var status BudgetStatus

	incomeStr, ok, err := app.storage.GetSetting(settingMonthlyIncome)
	if err != nil {
		return status, fmt.Errorf("error reading income: %w", err)
	}
	status.HasIncome = ok
	if !ok {
		return status, nil
	}

	income, err := strconv.ParseInt(incomeStr, 10, 64)
	if err != nil {
		return status, fmt.Errorf("incorrect income value in settings: %w", err)
	}
	status.MonthlyIncomeCents = income

	obligations, err := app.storage.ObligationsDueThisMonth(now)
	if err != nil {
		return status, fmt.Errorf("error getting obligations: %w", err)
	}
	for _, o := range obligations {
		status.ObligationsCents += o.Amount
	}

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)

	spent, err := app.storage.SumTransactionsInRange(monthStart.Unix(), monthEnd.Unix(), "expense")
	if err != nil {
		return status, fmt.Errorf("error calculating expenses: %w", err)
	}
	status.SpentCents = -spent // expense is stored as negative

	approvedPlanned, err := app.storage.SumPlannedExpensesByStatus("approved")
	if err != nil {
		return status, fmt.Errorf("error calculating approved planned expenses: %w", err)
	}
	status.ApprovedPlannedCents = approvedPlanned

	status.RemainingCents = status.MonthlyIncomeCents - status.ObligationsCents - status.SpentCents - status.ApprovedPlannedCents

	totalDaysInMonth := int(monthEnd.Sub(monthStart).Hours() / 24)
	daysLeft := totalDaysInMonth - now.Day() + 1
	if daysLeft < 1 {
		daysLeft = 1
	}
	status.DaysLeftInMonth = daysLeft

	if status.RemainingCents > 0 {
		status.SuggestedDailyLimit = status.RemainingCents / int64(daysLeft)
	}

	return status, nil
}

// formatBudgetStatus formats BudgetStatus into a text message for Telegram
func (app *App) formatBudgetStatus(s BudgetStatus) string {
	if !s.HasIncome {
		return "Monthly income is not set. Use /setincome <amount> so I can calculate the budget."
	}

	var sb strings.Builder
	sb.WriteString("💰 *Budget status for this month*\n")
	sb.WriteString(fmt.Sprintf("Income: %.2f UAH\n", float64(s.MonthlyIncomeCents)/100))
	sb.WriteString(fmt.Sprintf("Mandatory payments this month: %.2f UAH\n", float64(s.ObligationsCents)/100))
	if s.ApprovedPlannedCents > 0 {
		sb.WriteString(fmt.Sprintf("Approved planned expenses: %.2f UAH\n", float64(s.ApprovedPlannedCents)/100))
	}
	sb.WriteString(fmt.Sprintf("Spent this month: %.2f UAH\n", float64(s.SpentCents)/100))
	sb.WriteString(fmt.Sprintf("Days left in month: %d\n", s.DaysLeftInMonth))

	if s.RemainingCents > 0 {
		sb.WriteString(fmt.Sprintf("Remaining until end of month: %.2f UAH\n", float64(s.RemainingCents)/100))
		sb.WriteString(fmt.Sprintf("📊 Recommended daily limit: %.2f UAH", float64(s.SuggestedDailyLimit)/100))
	} else {
		sb.WriteString(fmt.Sprintf("⚠️ Based on current obligations and spending, predicted deficit by end of month: %.2f UAH", -float64(s.RemainingCents)/100))
	}

	return sb.String()
}

// buildAnalysisPrompt builds a structured request to the AI.
// In addition to transactions, it includes the current budget status, mandatory payments and
// planned one-time expenses so that the AI advice takes into account the full picture, not just a list of purchases.
func (app *App) buildAnalysisPrompt(txs []storage.Transaction, status BudgetStatus, obligations []storage.Obligation, pendingPlanned []storage.PlannedExpense) string {
	var sb strings.Builder
	sb.WriteString("You are a personal financial advisor. Analyze the new transactions in the context of the user's monthly budget and provide brief, specific and actionable advice in English: is the user staying within budget, is there a risk of going negative, and what can they afford in the near term. Format your response in Markdown for Telegram (bold text, lists), avoid complex formatting and unnecessary verbosity.\n\n")

	if status.HasIncome {
		sb.WriteString("### Budget for this month\n")
		sb.WriteString(fmt.Sprintf("- Income: %.2f UAH\n", float64(status.MonthlyIncomeCents)/100))
		sb.WriteString(fmt.Sprintf("- Mandatory payments this month: %.2f UAH\n", float64(status.ObligationsCents)/100))
		if status.ApprovedPlannedCents > 0 {
			sb.WriteString(fmt.Sprintf("- Approved planned expenses: %.2f UAH\n", float64(status.ApprovedPlannedCents)/100))
		}
		sb.WriteString(fmt.Sprintf("- Spent this month: %.2f UAH\n", float64(status.SpentCents)/100))
		sb.WriteString(fmt.Sprintf("- Days left in month: %d\n", status.DaysLeftInMonth))
		if status.RemainingCents > 0 {
			sb.WriteString(fmt.Sprintf("- Free balance: %.2f UAH (~%.2f UAH/day)\n", float64(status.RemainingCents)/100, float64(status.SuggestedDailyLimit)/100))
		} else {
			sb.WriteString(fmt.Sprintf("- WARNING: predicted deficit by end of month: %.2f UAH\n", -float64(status.RemainingCents)/100))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("### Budget\nMonthly income is not set, so evaluate the situation based on spending trends alone.\n\n")
	}

	if len(obligations) > 0 {
		sb.WriteString("### Mandatory payments this month\n")
		for _, o := range obligations {
			sb.WriteString(fmt.Sprintf("- %s: %.2f UAH (date: %s)\n", o.Name, float64(o.Amount)/100, o.NextDueDate))
		}
		sb.WriteString("\n")
	}

	if len(pendingPlanned) > 0 {
		sb.WriteString("### Planned one-time expenses awaiting user decision\n")
		for _, p := range pendingPlanned {
			sb.WriteString(fmt.Sprintf("- %s: %.2f UAH\n", p.Name, float64(p.Amount)/100))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("### New transactions\n")
	for _, tx := range txs {
		amountFormatted := float64(tx.Amount) / 100.0
		sign := "-"
		if tx.Type == "income" {
			sign = "+"
		}
		timeStr := time.Unix(tx.Timestamp, 0).Format("02.01 15:04")
		category := monobank.CategoryForMCC(tx.MCC)
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s%.2f UAH (category: %s, %s)\n",
			timeStr, tx.Description, sign, math.Abs(amountFormatted), category, tx.Type))
	}

	return sb.String()
}

// telegramUpdate describes the structure of an incoming request from Telegram Webhook
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

// handleTelegramWebhook handles incoming updates from Telegram bot (webhooks)
func (app *App) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var update telegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("Error decoding Telegram update: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if update.Message != nil && update.Message.Text != "" {
		log.Printf("Received message from chat %d: %s", update.Message.Chat.ID, update.Message.Text)

		if err := app.processCommand(update.Message.Chat.ID, update.Message.Text); err != nil {
			log.Printf("Error processing command: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// settingMonthlyIncome is the key in the settings table for expected monthly income (in kopiykas)
const settingMonthlyIncome = "monthly_income"

// parseAmountToCents converts an amount entered by the user (e.g. "45000" or "45000.50") to kopiykas
func parseAmountToCents(s string) (int64, error) {
	value, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("incorrect amount: %s", s)
	}
	return int64(math.Round(value * 100)), nil
}

// processCommand processes text commands received from Telegram bot
func (app *App) processCommand(chatID int64, text string) error {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	parts := strings.Fields(text)
	cmd := parts[0]

	switch cmd {
	case "/setincome":
		if len(parts) != 2 {
			return app.tg.SendMessage("Usage: /setincome <amount in UAH>, e.g. /setincome 45000")
		}
		cents, err := parseAmountToCents(parts[1])
		if err != nil {
			return app.tg.SendMessage("Incorrect amount. Example: /setincome 45000")
		}
		if err := app.storage.SetSetting(settingMonthlyIncome, strconv.FormatInt(cents, 10)); err != nil {
			return fmt.Errorf("error saving income: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("✅ Monthly income set: %.2f UAH", float64(cents)/100))

	case "/income":
		value, ok, err := app.storage.GetSetting(settingMonthlyIncome)
		if err != nil {
			return fmt.Errorf("error reading income: %w", err)
		}
		if !ok {
			return app.tg.SendMessage("Monthly income is not set yet. Use /setincome <amount>")
		}
		cents, _ := strconv.ParseInt(value, 10, 64)
		return app.tg.SendMessage(fmt.Sprintf("Current monthly income: %.2f UAH", float64(cents)/100))

	case "/addobligation":
		args := parts[1:]
		if len(args) < 4 {
			return app.tg.SendMessage("Usage: /addobligation <name> <amount> <interval_months> <date YYYY-MM-DD>\nExample: /addobligation Parents 4000 1 2026-10-01")
		}

		dateStr := args[len(args)-1]
		intervalStr := args[len(args)-2]
		amountStr := args[len(args)-3]
		name := strings.Join(args[:len(args)-3], " ")

		amountCents, err := parseAmountToCents(amountStr)
		if err != nil {
			return app.tg.SendMessage("Incorrect amount. Example: /addobligation Parents 4000 1 2026-10-01")
		}

		interval, err := strconv.Atoi(intervalStr)
		if err != nil || interval <= 0 {
			return app.tg.SendMessage("Incorrect interval — expected positive number of months (1 = monthly, 3 = every 3 months).")
		}

		if _, err := time.Parse("2006-01-02", dateStr); err != nil {
			return app.tg.SendMessage("Incorrect date for next payment. Format: YYYY-MM-DD")
		}

		id, err := app.storage.AddObligation(name, amountCents, interval, dateStr)
		if err != nil {
			return fmt.Errorf("error saving obligation: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("✅ Added obligation #%d: %s — %.2f UAH every %d months (next payment: %s)",
			id, name, float64(amountCents)/100, interval, dateStr))

	case "/listobligations":
		obligations, err := app.storage.ListObligations(true)
		if err != nil {
			return fmt.Errorf("error getting obligations: %w", err)
		}
		if len(obligations) == 0 {
			return app.tg.SendMessage("No active mandatory payments. Add one with /addobligation.")
		}

		var sb strings.Builder
		sb.WriteString("📋 Active mandatory payments:\n")
		for _, o := range obligations {
			sb.WriteString(fmt.Sprintf("#%d %s — %.2f UAH every %d months (next: %s)\n",
				o.ID, o.Name, float64(o.Amount)/100, o.IntervalMonths, o.NextDueDate))
		}
		return app.tg.SendMessage(sb.String())

	case "/removeobligation":
		if len(parts) != 2 {
			return app.tg.SendMessage("Usage: /removeobligation <id> (see id in /listobligations)")
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return app.tg.SendMessage("Incorrect ID.")
		}
		if err := app.storage.DeleteObligation(id); err != nil {
			return fmt.Errorf("error deleting obligation: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("🗑 Obligation #%d deleted.", id))

	case "/status":
		status, err := app.CalculateBudgetStatus(time.Now())
		if err != nil {
			return fmt.Errorf("error calculating budget: %w", err)
		}
		return app.tg.SendMessage(app.formatBudgetStatus(status))

	case "/addplanned":
		args := parts[1:]
		if len(args) < 2 {
			return app.tg.SendMessage("Usage: /addplanned <name> <amount>\nExample: /addplanned Gift for friend 5000")
		}

		amountStr := args[len(args)-1]
		name := strings.Join(args[:len(args)-1], " ")

		amountCents, err := parseAmountToCents(amountStr)
		if err != nil {
			return app.tg.SendMessage("Incorrect amount. Example: /addplanned Gift for friend 5000")
		}

		id, err := app.storage.AddPlannedExpense(name, amountCents, "")
		if err != nil {
			return fmt.Errorf("error saving planned expense: %w", err)
		}

		status, err := app.CalculateBudgetStatus(time.Now())
		if err != nil {
			return fmt.Errorf("error calculating budget: %w", err)
		}

		return app.tg.SendMessage(app.formatPlannedExpenseVerdict(id, name, amountCents, status))

	case "/confirmplanned":
		if len(parts) != 2 {
			return app.tg.SendMessage("Usage: /confirmplanned <id>")
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return app.tg.SendMessage("Incorrect ID.")
		}
		if err := app.storage.UpdatePlannedExpenseStatus(id, "approved"); err != nil {
			return fmt.Errorf("error confirming planned expense: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("✅ Planned expense #%d confirmed — I'll include it in this month's budget.", id))

	case "/cancelplanned":
		if len(parts) != 2 {
			return app.tg.SendMessage("Usage: /cancelplanned <id>")
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return app.tg.SendMessage("Incorrect ID.")
		}
		if err := app.storage.UpdatePlannedExpenseStatus(id, "rejected"); err != nil {
			return fmt.Errorf("error canceling planned expense: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("🗑 Planned expense #%d canceled.", id))

	case "/listplanned":
		pending, err := app.storage.ListPlannedExpenses("pending")
		if err != nil {
			return fmt.Errorf("error getting planned expenses: %w", err)
		}
		approved, err := app.storage.ListPlannedExpenses("approved")
		if err != nil {
			return fmt.Errorf("error getting planned expenses: %w", err)
		}
		if len(pending) == 0 && len(approved) == 0 {
			return app.tg.SendMessage("No active planned expenses.")
		}

		var sb strings.Builder
		if len(pending) > 0 {
			sb.WriteString("⏳ Awaiting decision:\n")
			for _, p := range pending {
				sb.WriteString(fmt.Sprintf("#%d %s — %.2f UAH\n", p.ID, p.Name, float64(p.Amount)/100))
			}
			sb.WriteString("\n")
		}
		if len(approved) > 0 {
			sb.WriteString("✅ Confirmed (included in budget):\n")
			for _, p := range approved {
				sb.WriteString(fmt.Sprintf("#%d %s — %.2f UAH\n", p.ID, p.Name, float64(p.Amount)/100))
			}
		}
		return app.tg.SendMessage(sb.String())

	default:
		return app.tg.SendMessage("Unknown command. Available: /setincome, /income, /addobligation, /listobligations, /removeobligation, /status, /addplanned, /confirmplanned, /cancelplanned, /listplanned")
	}
}

// formatPlannedExpenseVerdict provides advice on a planned one-time expense.
// This is advice, not a prohibition — the final decision is always up to the user.
func (app *App) formatPlannedExpenseVerdict(id int64, name string, amountCents int64, status BudgetStatus) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎁 Planned expense #%d: %s — %.2f UAH\n\n", id, name, float64(amountCents)/100))

	if !status.HasIncome {
		sb.WriteString("I don't know your monthly income (/setincome), so I can't assess affordability — I've recorded it, but the decision is yours.")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Available until end of month (accounting for obligations, spending and already confirmed plans): %.2f UAH over %d days.\n\n",
		float64(status.RemainingCents)/100, status.DaysLeftInMonth))

	if status.RemainingCents >= amountCents {
		afterCents := status.RemainingCents - amountCents
		perDay := float64(afterCents) / float64(status.DaysLeftInMonth) / 100
		sb.WriteString(fmt.Sprintf("✅ This is affordable: after this expense, %.2f UAH (~%.2f UAH/day) will remain until end of month.",
			float64(afterCents)/100, perDay))
	} else {
		affordableNow := math.Max(0, float64(status.RemainingCents)/100)
		sb.WriteString(fmt.Sprintf("⚠️ This exceeds your free balance. Without affecting other expenses, you can allocate approximately %.2f UAH now.\n", affordableNow))
		sb.WriteString("This is not a prohibition — you can consciously reduce something else this month and complete the full amount, or reduce the amount now. The choice is yours.\n")
	}

	sb.WriteString(fmt.Sprintf("\nTo include this expense in the budget: /confirmplanned %d\nTo cancel: /cancelplanned %d", id, id))
	return sb.String()
}

func main() {
	// Load .env file if it exists (for local development)
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

	// Check for required environment variables
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
			log.Fatalf("Error initializing app: %v", initErr)
		}
		log.Println("All financial bot modules successfully initialized!")
	} else {
		log.Printf("Warning: Bot started in limited demo mode (not all environment variables present: %s)", strings.Join(missing, ", "))
		log.Println("Please fill them in for full functionality.")
	}

	// Healthcheck for Fly.io (always works so Fly.io doesn't restart container due to failed deploy)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Endpoint for manual synchronization trigger
	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if app == nil {
			http.Error(w, "Error: Bot not initialized. Check configuration.", http.StatusServiceUnavailable)
			return
		}

		days := 7 // By default, get statement for last week if database is empty
		if daysParam := r.URL.Query().Get("days"); daysParam != "" {
			parsedDays, err := strconv.Atoi(daysParam)
			if err != nil || parsedDays <= 0 {
				http.Error(w, "Incorrect days parameter: expected positive number", http.StatusBadRequest)
				return
			}
			days = parsedDays
		}

		err := app.Sync(days)
		if err != nil {
			log.Printf("Error during synchronization: %v", err)
			http.Error(w, fmt.Sprintf("Synchronization failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Synchronization successful! Report sent to Telegram."))
	})

	// Endpoint for Telegram webhooks (for receiving commands)
	http.HandleFunc("/telegram/webhook", func(w http.ResponseWriter, r *http.Request) {
		if app == nil {
			http.Error(w, "Error: Bot not initialized.", http.StatusServiceUnavailable)
			return
		}
		app.handleTelegramWebhook(w, r)
	})

	log.Printf("Financial advisor server started on port %s\n", port)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}
