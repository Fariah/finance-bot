# Finance Bot - Personal Financial Advisor 💰

A Go-based financial advisor that analyzes your Monobank transactions, tracks your budget, and provides AI-powered financial advice. It syncs bank data, stores it locally in SQLite, and uses Claude AI to deliver actionable insights via Telegram.

## Features

- ✅ **One-Click Analysis** - Run `finance-bot.exe` and get instant financial report
- 💰 **Annual Budget Tracking** - Calculates yearly balance projections based on current spending patterns
- 🤖 **AI-Powered Analysis** - Claude AI identifies spending trends by category and flags excessive expenses
- 💬 **Telegram Reports** - Formatted financial analysis delivered directly to Telegram
- 📊 **Category Breakdown** - Automatic spending categorization (Food, Transport, Entertainment, subscriptions, etc.)
- 🎯 **Monthly Balance** - Income minus expenses minus mandatory savings with annual forecast
- 💾 **Local SQLite Database** - All data stored locally, no cloud required

## Quick Start

### Prerequisites

- Go 1.20+ (for building) or pre-built `finance-bot.exe`
- Monobank account with personal API token from https://api.monobank.ua/
- Telegram bot token from @BotFather
- Claude API key from https://console.anthropic.com/

### Setup & Run

1. **Clone repository**
   ```bash
   git clone <repo-url>
   cd finance-bot
   ```

2. **Create `.env` file**
   ```bash
   cp .env.example .env
   ```

3. **Edit `.env` with your API keys:**
   ```env
   MONOBANK_TOKEN=your_monobank_token
   MONOBANK_ACCOUNT_ID=your_account_id
   TELEGRAM_BOT_TOKEN=your_telegram_bot_token
   TELEGRAM_CHAT_ID=your_chat_id
   ANTHROPIC_API_KEY=your_claude_api_key
   MONTHLY_INCOME=15500000          # Your monthly income in kopiykas
   MANDATORY_SAVINGS=800000         # Monthly savings target in kopiykas
   ```

4. **Build** (one-time)
   ```bash
   go build -o finance-bot.exe main.go
   ```

5. **Run analysis**
   ```bash
   .\finance-bot.exe
   ```

The app will:
- 📥 Fetch last 31 days from Monobank
- 💾 Save transactions to local SQLite database
- 🤖 Analyze with Claude AI
- 📤 Send report to Telegram

**That's it! No server needed, no parameters required.**

## Usage

### Default Mode: One-Shot Analysis
```bash
.\finance-bot.exe
```
Analyzes last 31 days, updates local database, sends report to Telegram, then exits. Perfect for:
- Running manually whenever needed
- Scheduling with Windows Task Scheduler or cron
- Daily reports via system scheduler

### Advanced: HTTP Server Mode
```bash
SERVER=1 .\finance-bot.exe
```
Starts HTTP server on port 8080 for programmatic triggering.

**Endpoints:**
- `GET /health` - Returns `OK` (liveness check)
- `GET /sync?days=7` - Trigger analysis for N days (1-31)

## Telegram Commands

### Budget Management
- **`/setincome <amount>`** - Set your monthly income (e.g., `/setincome 45000`)
- **`/income`** - Display current monthly income setting
- **`/status`** - Show current budget status (income, obligations, spent, remaining, suggested daily limit)

### Recurring Payments
- **`/addobligation <name> <amount> <interval_months> <date YYYY-MM-DD>`** - Add a recurring payment
  - Example: `/addobligation Parents 4000 1 2026-10-01` (monthly payment starting Oct 1)
- **`/listobligations`** - List all active mandatory payments
- **`/removeobligation <id>`** - Remove a recurring payment by ID

### Planned Expenses
- **`/addplanned <name> <amount>`** - Propose a one-time expense
  - Example: `/addplanned Gift for friend 5000`
  - Bot will analyze your budget and advise on affordability
- **`/confirmplanned <id>`** - Approve a planned expense (includes it in budget calculations)
- **`/cancelplanned <id>`** - Reject a planned expense
- **`/listplanned`** - Show pending and confirmed planned expenses

## Architecture

### Project Structure
```
finance-bot/
├── main.go                      # Core sync & analysis logic
├── storage/
│   └── sqlite.go                # SQLite database layer
├── monobank/
│   ├── client.go                # Monobank API client
│   └── mcc.go                   # Merchant category codes
├── telegram/
│   └── client.go                # Telegram Bot API client
├── llm/
│   └── client.go                # Claude API integration
├── CLAUDE.md                    # Development guide
├── .env.example                 # Configuration template
├── .gitignore                   # Git ignore rules
└── README.md                    # This file
```

### How It Works

1. **Run the app** - `.\finance-bot.exe`
2. **Fetch from Monobank** - Pulls last 31 days of transactions
3. **Save to Database** - SQLite stores transactions (no duplicates)
4. **Analyze** - Claude AI processes entire current month's data
5. **Send Report** - Telegram receives formatted analysis
6. **Exit** - App finishes and closes

**Scheduling:**
- Run manually whenever needed
- Or schedule with system scheduler (Windows Task Scheduler / cron)
- Run daily/weekly as preferred

### Core Components

**App (main.go)**
- Central orchestrator that coordinates all components
- Manages HTTP server and endpoints
- Implements business logic for sync pipeline and budget calculations
- Processes Telegram commands

**Storage (storage/sqlite.go)**
- Manages SQLite database connection and schema
- Implements CRUD operations for transactions, obligations, planned expenses, and settings
- Handles recurring payment date rolling (automatic forward-dating of overdue obligations)

**Monobank Client (monobank/client.go)**
- Fetches bank statements via Monobank Personal API
- Respects 31-day API limit
- Parses MCC (Merchant Category Code) from transactions

**Telegram Client (telegram/client.go)**
- Sends formatted messages to Telegram chat
- Supports Markdown formatting for bold text and lists
- Handles bot token authentication

**LLM Client (llm/client.go)**
- Sends structured prompts to Anthropic Claude API (using Haiku for efficiency)
- Receives AI-generated financial analysis in Ukrainian
- Handles API errors gracefully

## Database Schema

### transactions
Stores individual bank transactions:
- `id` (TEXT PRIMARY KEY) - Unique transaction ID from Monobank
- `amount` (INTEGER) - Amount in kopiykas (cents); negative = expense, positive = income
- `description` (TEXT) - Transaction description (e.g., "AUCHAN Kyiv")
- `mcc` (INTEGER) - Merchant Category Code for categorization
- `timestamp` (INTEGER) - Unix timestamp
- `type` (TEXT) - Either "income" or "expense"

### obligations
Recurring mandatory payments (bills, subscriptions, transfers):
- `id` (INTEGER PRIMARY KEY)
- `name` (TEXT) - Payment name (e.g., "Internet Bill")
- `amount` (INTEGER) - Amount in kopiykas
- `interval_months` (INTEGER) - Recurrence interval (1 = monthly, 3 = quarterly)
- `next_due_date` (TEXT) - Next payment date in YYYY-MM-DD format
- `active` (INTEGER) - 1 = active, 0 = deleted

### planned_expenses
One-time expenses awaiting user decision:
- `id` (INTEGER PRIMARY KEY)
- `name` (TEXT) - Expense name (e.g., "Birthday gift")
- `amount` (INTEGER) - Amount in kopiykas
- `status` (TEXT) - "pending" (awaiting approval), "approved" (confirmed), or "rejected"
- `note` (TEXT) - Optional user notes
- `created_at` (INTEGER) - Creation timestamp

### settings
Key-value configuration store:
- `key` (TEXT PRIMARY KEY)
- `value` (TEXT)

Current keys:
- `monthly_income` - User's monthly income in kopiykas

### monthly_stats
Reserved for future use (populated but currently unused):
- `month` (TEXT PRIMARY KEY) - Month in YYYY-MM format
- `total_income` (INTEGER) - Total income for the month
- `total_expense` (INTEGER) - Total expenses for the month
- `fop_tax_reserved` (INTEGER) - Tax reserves (for freelancers)

## Development

### Build
```bash
go build -o finance-bot.exe main.go
```

### Run with Code Changes
```bash
go run main.go
```

### Testing Locally
1. Set up `.env` with test API keys
2. Run: `go run main.go`
3. Check logs to verify:
   - Monobank connection ✓
   - Claude AI connection ✓
   - Telegram delivery ✓

### Debug Mode: HTTP Server
```bash
SERVER=1 go run main.go
```
Then test manually:
```bash
curl http://localhost:8080/health
curl http://localhost:8080/sync?days=7
```

### Code Conventions

- **Language**: English for all code, comments, error messages, and logs
- **Monetary Values**: Stored as `int64` integers representing kopiykas (e.g., 100 UAH = 10000 kopiykas)
  - Convert for display: `amount / 100` to get UAH
  - Positive values = income, negative values = expense
- **Error Handling**: Always wrap errors using `%w` to preserve context
- **No CGO**: Uses `modernc.org/sqlite` (pure Go driver) for cross-platform compatibility
- **API Limits**: Monobank statement requests are limited to 31 days per request; the app enforces this automatically

## Scheduling Analysis

### Windows: Task Scheduler
Create a scheduled task to run daily:
```powershell
$trigger = New-ScheduledTaskTrigger -Daily -At 8:00am
$action = New-ScheduledTaskAction -Execute "C:\path\to\finance-bot.exe"
Register-ScheduledTask -TaskName "FinanceBot Daily" -Trigger $trigger -Action $action
```

### Linux/macOS: Cron
Add to crontab:
```bash
0 8 * * * cd /path/to/finance-bot && ./finance-bot
```

Runs daily at 8 AM and sends Telegram report.


## Configuration

### Monobank API
1. Get your personal token from https://api.monobank.ua/
2. Find your account ID in Monobank settings or from API `/personal/client-info`
3. Set `MONOBANK_TOKEN` and `MONOBANK_ACCOUNT_ID` in `.env`

### Telegram Bot
1. Create a bot via [@BotFather](https://t.me/botfather)
2. Get your Chat ID: send `/start` to your bot and note the `chat.id` from the update
3. Set `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` in `.env`

### Claude AI (Anthropic)
1. Get your API key from https://console.anthropic.com/
2. Set `ANTHROPIC_API_KEY` in `.env`

### Monthly Income & Savings
Set these in `.env` for accurate budget projections:
```env
MONTHLY_INCOME=15500000        # Your monthly income in kopiykas
MANDATORY_SAVINGS=800000       # Monthly savings target in kopiykas
```

These values are loaded at startup and automatically saved to the local database.

## Future Enhancements

- [ ] Unit tests with mocks for all packages
- [ ] Historical spending trends (compare month-to-month changes)
- [ ] Spending forecast (predict when you hit annual budget limits)
- [ ] Mobile app dashboard (view reports on phone)
- [ ] Transaction search and filtering interface
- [ ] Budget alerts when approaching limits
- [ ] Multi-account support (analyze multiple Monobank accounts)

## Troubleshooting

**"Missing required environment variables"**
- Check `.env` file has all required keys: `MONOBANK_TOKEN`, `MONOBANK_ACCOUNT_ID`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `ANTHROPIC_API_KEY`
- Ensure `.env` is in the same directory as `finance-bot.exe`

**"Monobank returned status: 429"**
- Rate limited. Monobank API has request limits. Wait before retrying.

**"Empty response from LLM"**
- Claude API request failed. Check your API key at https://console.anthropic.com/ and quota remaining.

**Reports not showing monthly income**
- Set your monthly income: `sqlite3 finance-bot.db "INSERT OR REPLACE INTO settings VALUES('monthly_income', 'YOUR_AMOUNT');"`
- Replace `YOUR_AMOUNT` with amount in kopiykas (e.g., 15500000 for 155,000 UAH)

**No transactions in report**
- Ensure you have transactions in Monobank for the analyzed period
- Check that `MONOBANK_ACCOUNT_ID` is correct

## License

Personal use. Ensure compliance with Monobank, Telegram, and Anthropic terms of service.

## Support

For issues or questions, open an issue in the repository.

---

**Built with Go** • **Monobank + Telegram + Claude AI**
