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

	log.Println("Розрахунок стану бюджету для промпту...")
	budgetStatus, err := app.CalculateBudgetStatus(now)
	if err != nil {
		log.Printf("Попередження: не вдалося розрахувати бюджет: %v", err)
	}

	obligationsDue, err := app.storage.ObligationsDueThisMonth(now)
	if err != nil {
		log.Printf("Попередження: не вдалося отримати обов'язкові платежі: %v", err)
	}

	pendingPlanned, err := app.storage.ListPlannedExpenses("pending")
	if err != nil {
		log.Printf("Попередження: не вдалося отримати заплановані витрати: %v", err)
	}

	log.Println("Формування аналітичного промпту для Gemini...")
	prompt := app.buildAnalysisPrompt(txsToAnalyze, budgetStatus, obligationsDue, pendingPlanned)

	log.Println("Запит аналізу у Gemini AI...")
	report, err := app.llm.Analyze(prompt)
	if err != nil {
		return fmt.Errorf("помилка генерації аналітики: %w", err)
	}

	log.Println("Надсилання звіту до Telegram...")
	if err := app.tg.SendMessage(report); err != nil {
		return fmt.Errorf("помилка надсилання в Telegram: %w", err)
	}

	if budgetStatus.HasIncome {
		if err := app.tg.SendMessage(app.formatBudgetStatus(budgetStatus)); err != nil {
			log.Printf("Помилка надсилання статусу бюджету: %v", err)
		}
	}

	log.Println("Синхронізацію успішно завершено!")
	return nil
}

// BudgetStatus описує розрахований стан бюджету на поточний місяць
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

// CalculateBudgetStatus рахує, скільки коштів лишається на місяць з урахуванням
// доходу, обов'язкових платежів цього місяця та вже витраченого
func (app *App) CalculateBudgetStatus(now time.Time) (BudgetStatus, error) {
	var status BudgetStatus

	incomeStr, ok, err := app.storage.GetSetting(settingMonthlyIncome)
	if err != nil {
		return status, fmt.Errorf("помилка читання доходу: %w", err)
	}
	status.HasIncome = ok
	if !ok {
		return status, nil
	}

	income, err := strconv.ParseInt(incomeStr, 10, 64)
	if err != nil {
		return status, fmt.Errorf("некоректне значення доходу в налаштуваннях: %w", err)
	}
	status.MonthlyIncomeCents = income

	obligations, err := app.storage.ObligationsDueThisMonth(now)
	if err != nil {
		return status, fmt.Errorf("помилка отримання обов'язків: %w", err)
	}
	for _, o := range obligations {
		status.ObligationsCents += o.Amount
	}

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)

	spent, err := app.storage.SumTransactionsInRange(monthStart.Unix(), monthEnd.Unix(), "expense")
	if err != nil {
		return status, fmt.Errorf("помилка підрахунку витрат: %w", err)
	}
	status.SpentCents = -spent // expense зберігається від'ємним

	approvedPlanned, err := app.storage.SumPlannedExpensesByStatus("approved")
	if err != nil {
		return status, fmt.Errorf("помилка підрахунку підтверджених планових витрат: %w", err)
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

// formatBudgetStatus форматує BudgetStatus у текстове повідомлення для Telegram
func (app *App) formatBudgetStatus(s BudgetStatus) string {
	if !s.HasIncome {
		return "Місячний дохід не встановлено. Використай /setincome <сума>, щоб я міг рахувати бюджет."
	}

	var sb strings.Builder
	sb.WriteString("💰 *Стан бюджету цього місяця*\n")
	sb.WriteString(fmt.Sprintf("Дохід: %.2f грн\n", float64(s.MonthlyIncomeCents)/100))
	sb.WriteString(fmt.Sprintf("Обов'язкові платежі цього місяця: %.2f грн\n", float64(s.ObligationsCents)/100))
	if s.ApprovedPlannedCents > 0 {
		sb.WriteString(fmt.Sprintf("Підтверджені заплановані витрати: %.2f грн\n", float64(s.ApprovedPlannedCents)/100))
	}
	sb.WriteString(fmt.Sprintf("Витрачено цього місяця: %.2f грн\n", float64(s.SpentCents)/100))
	sb.WriteString(fmt.Sprintf("Днів до кінця місяця: %d\n", s.DaysLeftInMonth))

	if s.RemainingCents > 0 {
		sb.WriteString(fmt.Sprintf("Залишок до кінця місяця: %.2f грн\n", float64(s.RemainingCents)/100))
		sb.WriteString(fmt.Sprintf("📊 Рекомендований ліміт на день: %.2f грн", float64(s.SuggestedDailyLimit)/100))
	} else {
		sb.WriteString(fmt.Sprintf("⚠️ За поточними обов'язками і витратами прогнозований мінус до кінця місяця: %.2f грн", -float64(s.RemainingCents)/100))
	}

	return sb.String()
}

// buildAnalysisPrompt формує структурований запит до штучного інтелекту.
// Крім самих транзакцій, включає поточний стан бюджету, обов'язкові платежі та
// заплановані разові витрати, щоб порада ШІ враховувала реальну картину, а не лише список покупок.
func (app *App) buildAnalysisPrompt(txs []storage.Transaction, status BudgetStatus, obligations []storage.Obligation, pendingPlanned []storage.PlannedExpense) string {
	var sb strings.Builder
	sb.WriteString("Ти — особистий фінансовий радник користувача. Проаналізуй нові транзакції у контексті його бюджету на місяць і дай короткі, конкретні та дієві поради українською мовою: чи вкладається він у бюджет, чи є ризик вийти в мінус, і чи можна собі щось дозволити найближчим часом. Форматуй відповідь у Markdown для Telegram (жирний текст, списки), уникай складного форматування та зайвої води.\n\n")

	if status.HasIncome {
		sb.WriteString("### Бюджет на цей місяць\n")
		sb.WriteString(fmt.Sprintf("- Дохід: %.2f грн\n", float64(status.MonthlyIncomeCents)/100))
		sb.WriteString(fmt.Sprintf("- Обов'язкові платежі цього місяця: %.2f грн\n", float64(status.ObligationsCents)/100))
		if status.ApprovedPlannedCents > 0 {
			sb.WriteString(fmt.Sprintf("- Підтверджені заплановані витрати: %.2f грн\n", float64(status.ApprovedPlannedCents)/100))
		}
		sb.WriteString(fmt.Sprintf("- Витрачено цього місяця: %.2f грн\n", float64(status.SpentCents)/100))
		sb.WriteString(fmt.Sprintf("- Днів до кінця місяця: %d\n", status.DaysLeftInMonth))
		if status.RemainingCents > 0 {
			sb.WriteString(fmt.Sprintf("- Вільний залишок: %.2f грн (~%.2f грн/день)\n", float64(status.RemainingCents)/100, float64(status.SuggestedDailyLimit)/100))
		} else {
			sb.WriteString(fmt.Sprintf("- УВАГА: прогнозований мінус до кінця місяця: %.2f грн\n", -float64(status.RemainingCents)/100))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("### Бюджет\nМісячний дохід не налаштовано, тому оцінюй ситуацію лише за динамікою витрат.\n\n")
	}

	if len(obligations) > 0 {
		sb.WriteString("### Обов'язкові платежі цього місяця\n")
		for _, o := range obligations {
			sb.WriteString(fmt.Sprintf("- %s: %.2f грн (дата: %s)\n", o.Name, float64(o.Amount)/100, o.NextDueDate))
		}
		sb.WriteString("\n")
	}

	if len(pendingPlanned) > 0 {
		sb.WriteString("### Заплановані разові витрати, що очікують рішення користувача\n")
		for _, p := range pendingPlanned {
			sb.WriteString(fmt.Sprintf("- %s: %.2f грн\n", p.Name, float64(p.Amount)/100))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("### Нові транзакції\n")
	for _, tx := range txs {
		amountFormatted := float64(tx.Amount) / 100.0
		sign := "-"
		if tx.Type == "income" {
			sign = "+"
		}
		timeStr := time.Unix(tx.Timestamp, 0).Format("02.01 15:04")
		category := monobank.CategoryForMCC(tx.MCC)
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s%.2f UAH (категорія: %s, %s)\n",
			timeStr, tx.Description, sign, math.Abs(amountFormatted), category, tx.Type))
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

		if err := app.processCommand(update.Message.Chat.ID, update.Message.Text); err != nil {
			log.Printf("Помилка обробки команди: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// settingMonthlyIncome — ключ у таблиці settings для очікуваного місячного доходу (у копійках)
const settingMonthlyIncome = "monthly_income"

// parseAmountToCents перетворює введену користувачем суму (напр. "45000" або "45000.50") у копійки
func parseAmountToCents(s string) (int64, error) {
	value, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("некоректна сума: %s", s)
	}
	return int64(math.Round(value * 100)), nil
}

// processCommand обробляє текстові команди, що надходять від Telegram бота
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
			return app.tg.SendMessage("Використання: /setincome <сума у грн>, напр. /setincome 45000")
		}
		cents, err := parseAmountToCents(parts[1])
		if err != nil {
			return app.tg.SendMessage("Некоректна сума. Приклад: /setincome 45000")
		}
		if err := app.storage.SetSetting(settingMonthlyIncome, strconv.FormatInt(cents, 10)); err != nil {
			return fmt.Errorf("помилка збереження доходу: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("✅ Місячний дохід встановлено: %.2f грн", float64(cents)/100))

	case "/income":
		value, ok, err := app.storage.GetSetting(settingMonthlyIncome)
		if err != nil {
			return fmt.Errorf("помилка читання доходу: %w", err)
		}
		if !ok {
			return app.tg.SendMessage("Місячний дохід ще не встановлено. Використай /setincome <сума>")
		}
		cents, _ := strconv.ParseInt(value, 10, 64)
		return app.tg.SendMessage(fmt.Sprintf("Поточний місячний дохід: %.2f грн", float64(cents)/100))

	case "/addobligation":
		args := parts[1:]
		if len(args) < 4 {
			return app.tg.SendMessage("Використання: /addobligation <назва> <сума> <інтервал_міс> <дата YYYY-MM-DD>\nПриклад: /addobligation Батьки 4000 1 2026-10-01")
		}

		dateStr := args[len(args)-1]
		intervalStr := args[len(args)-2]
		amountStr := args[len(args)-3]
		name := strings.Join(args[:len(args)-3], " ")

		amountCents, err := parseAmountToCents(amountStr)
		if err != nil {
			return app.tg.SendMessage("Некоректна сума. Приклад: /addobligation Батьки 4000 1 2026-10-01")
		}

		interval, err := strconv.Atoi(intervalStr)
		if err != nil || interval <= 0 {
			return app.tg.SendMessage("Некоректний інтервал — очікується додатне число місяців (1 = щомісяця, 3 = раз на 3 місяці).")
		}

		if _, err := time.Parse("2006-01-02", dateStr); err != nil {
			return app.tg.SendMessage("Некоректна дата наступного платежу. Формат: YYYY-MM-DD")
		}

		id, err := app.storage.AddObligation(name, amountCents, interval, dateStr)
		if err != nil {
			return fmt.Errorf("помилка збереження обов'язку: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("✅ Додано обов'язок #%d: %s — %.2f грн кожні %d міс. (наступний платіж: %s)",
			id, name, float64(amountCents)/100, interval, dateStr))

	case "/listobligations":
		obligations, err := app.storage.ListObligations(true)
		if err != nil {
			return fmt.Errorf("помилка отримання обов'язків: %w", err)
		}
		if len(obligations) == 0 {
			return app.tg.SendMessage("Активних обов'язкових платежів немає. Додай через /addobligation.")
		}

		var sb strings.Builder
		sb.WriteString("📋 Активні обов'язкові платежі:\n")
		for _, o := range obligations {
			sb.WriteString(fmt.Sprintf("#%d %s — %.2f грн кожні %d міс. (наступний: %s)\n",
				o.ID, o.Name, float64(o.Amount)/100, o.IntervalMonths, o.NextDueDate))
		}
		return app.tg.SendMessage(sb.String())

	case "/removeobligation":
		if len(parts) != 2 {
			return app.tg.SendMessage("Використання: /removeobligation <id> (id дивись у /listobligations)")
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return app.tg.SendMessage("Некоректний ID.")
		}
		if err := app.storage.DeleteObligation(id); err != nil {
			return fmt.Errorf("помилка видалення обов'язку: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("🗑 Обов'язок #%d видалено.", id))

	case "/status":
		status, err := app.CalculateBudgetStatus(time.Now())
		if err != nil {
			return fmt.Errorf("помилка розрахунку бюджету: %w", err)
		}
		return app.tg.SendMessage(app.formatBudgetStatus(status))

	case "/addplanned":
		args := parts[1:]
		if len(args) < 2 {
			return app.tg.SendMessage("Використання: /addplanned <назва> <сума>\nПриклад: /addplanned Подарунок другу 5000")
		}

		amountStr := args[len(args)-1]
		name := strings.Join(args[:len(args)-1], " ")

		amountCents, err := parseAmountToCents(amountStr)
		if err != nil {
			return app.tg.SendMessage("Некоректна сума. Приклад: /addplanned Подарунок другу 5000")
		}

		id, err := app.storage.AddPlannedExpense(name, amountCents, "")
		if err != nil {
			return fmt.Errorf("помилка збереження запланованої витрати: %w", err)
		}

		status, err := app.CalculateBudgetStatus(time.Now())
		if err != nil {
			return fmt.Errorf("помилка розрахунку бюджету: %w", err)
		}

		return app.tg.SendMessage(app.formatPlannedExpenseVerdict(id, name, amountCents, status))

	case "/confirmplanned":
		if len(parts) != 2 {
			return app.tg.SendMessage("Використання: /confirmplanned <id>")
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return app.tg.SendMessage("Некоректний ID.")
		}
		if err := app.storage.UpdatePlannedExpenseStatus(id, "approved"); err != nil {
			return fmt.Errorf("помилка підтвердження запланованої витрати: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("✅ Заплановану витрату #%d підтверджено — врахую її в бюджеті цього місяця.", id))

	case "/cancelplanned":
		if len(parts) != 2 {
			return app.tg.SendMessage("Використання: /cancelplanned <id>")
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return app.tg.SendMessage("Некоректний ID.")
		}
		if err := app.storage.UpdatePlannedExpenseStatus(id, "rejected"); err != nil {
			return fmt.Errorf("помилка скасування запланованої витрати: %w", err)
		}
		return app.tg.SendMessage(fmt.Sprintf("🗑 Заплановану витрату #%d скасовано.", id))

	case "/listplanned":
		pending, err := app.storage.ListPlannedExpenses("pending")
		if err != nil {
			return fmt.Errorf("помилка отримання планових витрат: %w", err)
		}
		approved, err := app.storage.ListPlannedExpenses("approved")
		if err != nil {
			return fmt.Errorf("помилка отримання планових витрат: %w", err)
		}
		if len(pending) == 0 && len(approved) == 0 {
			return app.tg.SendMessage("Немає активних запланованих витрат.")
		}

		var sb strings.Builder
		if len(pending) > 0 {
			sb.WriteString("⏳ Очікують рішення:\n")
			for _, p := range pending {
				sb.WriteString(fmt.Sprintf("#%d %s — %.2f грн\n", p.ID, p.Name, float64(p.Amount)/100))
			}
			sb.WriteString("\n")
		}
		if len(approved) > 0 {
			sb.WriteString("✅ Підтверджені (враховані в бюджеті):\n")
			for _, p := range approved {
				sb.WriteString(fmt.Sprintf("#%d %s — %.2f грн\n", p.ID, p.Name, float64(p.Amount)/100))
			}
		}
		return app.tg.SendMessage(sb.String())

	default:
		return app.tg.SendMessage("Невідома команда. Доступно: /setincome, /income, /addobligation, /listobligations, /removeobligation, /status, /addplanned, /confirmplanned, /cancelplanned, /listplanned")
	}
}

// formatPlannedExpenseVerdict формує пораду щодо запланованої разової витрати.
// Це саме порада, а не заборона — остаточне рішення завжди за користувачем.
func (app *App) formatPlannedExpenseVerdict(id int64, name string, amountCents int64, status BudgetStatus) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🎁 Запланована витрата #%d: %s — %.2f грн\n\n", id, name, float64(amountCents)/100))

	if !status.HasIncome {
		sb.WriteString("Я не знаю твій місячний дохід (/setincome), тому не можу оцінити підйомність — записав, але рішення за тобою.")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Вільно до кінця місяця (з урахуванням обов'язків, витрат і вже підтверджених планів): %.2f грн на %d днів.\n\n",
		float64(status.RemainingCents)/100, status.DaysLeftInMonth))

	if status.RemainingCents >= amountCents {
		afterCents := status.RemainingCents - amountCents
		perDay := float64(afterCents) / float64(status.DaysLeftInMonth) / 100
		sb.WriteString(fmt.Sprintf("✅ Це підйомно: після цієї витрати лишиться %.2f грн (~%.2f грн/день) до кінця місяця.",
			float64(afterCents)/100, perDay))
	} else {
		affordableNow := math.Max(0, float64(status.RemainingCents)/100)
		sb.WriteString(fmt.Sprintf("⚠️ Це більше, ніж вільний залишок. Без шкоди для інших витрат зараз можна виділити приблизно %.2f грн.\n", affordableNow))
		sb.WriteString("Це не заборона — можеш свідомо скоротити щось інше цього місяця і виконати повну суму, або зменшити суму зараз. Рішення за тобою.\n")
	}

	sb.WriteString(fmt.Sprintf("\nЩоб врахувати цю витрату в бюджеті: /confirmplanned %d\nЩоб скасувати: /cancelplanned %d", id, id))
	return sb.String()
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
		if daysParam := r.URL.Query().Get("days"); daysParam != "" {
			parsedDays, err := strconv.Atoi(daysParam)
			if err != nil || parsedDays <= 0 {
				http.Error(w, "Некоректний параметр days: очікується додатне число", http.StatusBadRequest)
				return
			}
			days = parsedDays
		}

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
