# Finance Bot - Personal Financial Advisor 💰

A Go-based financial advisor that analyzes your bank transactions, tracks your budget, and provides AI-powered financial advice in **Ukrainian**. It syncs transactions from Monobank, stores them in SQLite, and uses Claude AI to deliver actionable insights via Telegram.

## Features

- ✅ **One-Click Analysis** - Run `finance-bot.exe` and get instant financial report
- 📊 **Smart Budget Tracking** - Annual budget outlook, monthly balance, recurring payments, planned expenses
- 🤖 **AI-Powered Analysis** - Claude AI analyzes spending trends, categories, and identifies overspending areas
- 💬 **Telegram Reports** - Receive formatted analysis in Ukrainian directly to Telegram
- 📈 **Category Breakdown** - Automatic spending analysis by category (Food, Transport, Entertainment, etc.)
- 💡 **Trend Detection** - Identifies when spending patterns are unsustainable for annual budget goals
- 🏦 **Flexible Deployment** - Works locally, on Fly.io, or anywhere with Go runtime

## Quick Start

### Prerequisites

- Go 1.20+ (or use pre-built `finance-bot.exe`)
- Monobank account with personal API access token
- Telegram Bot (create via @BotFather)
- Anthropic Claude API key (https://console.anthropic.com/)

### Installation & First Run

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd finance-bot
   ```

2. **Set up environment**
   ```bash
   cp .env.example .env
   # Edit .env with your credentials
   ```

3. **Fill in `.env` with your API keys**
   ```env
   MONOBANK_TOKEN=your_monobank_token
   MONOBANK_ACCOUNT_ID=your_account_id
   TELEGRAM_BOT_TOKEN=your_bot_token
   TELEGRAM_CHAT_ID=your_chat_id
   ANTHROPIC_API_KEY=your_claude_api_key
   ```

4. **Build the application** (one time)
   ```bash
   go build -o finance-bot.exe main.go
   ```

5. **Run analysis** - Just double-click or run:
   ```bash
   .\finance-bot.exe
   ```
   
   That's it! The app will:
   - 📥 Fetch last 31 days from Monobank
   - 💾 Save/update transactions in local database
   - 🤖 Analyze with Claude AI
   - 📤 Send formatted report to Telegram
   
   **No parameters needed, no server running, just pure analysis!**

## Usage Modes

### Local Analysis Mode (Default)
```bash
.\finance-bot.exe
```
Runs once, analyzes 31 days, sends report to Telegram, exits. Perfect for daily use.

### HTTP Server Mode (For Deployment)
```bash
SERVER=1 .\finance-bot.exe
```
Starts HTTP server on port 8080. Available endpoints:

#### `/health`
```
GET /health
```
Liveness check. Returns `OK` with HTTP 200 (for deployment monitoring).

#### `/sync?days=31`
```
GET /sync?days=31
```
Manually trigger sync and analysis. Useful for Fly.io Machines scheduler.

**Query Parameters:**
- `days` (optional): Number of days to analyze (default: 7, max: 31)

**Response:**
- `200 OK`: "Synchronization successful! Report sent to Telegram."
- `503 Service Unavailable`: Missing environment variables
- `400 Bad Request`: Invalid parameters

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
├── main.go                      # HTTP server and request handlers
├── storage/
│   └── sqlite.go                # SQLite database layer
├── monobank/
│   ├── client.go                # Monobank API integration
│   └── mcc.go                   # Merchant category mapping
├── telegram/
│   └── client.go                # Telegram Bot API client
├── llm/
│   └── client.go                # Google Gemini AI integration
├── fly.toml                     # Fly.io configuration
├── FLY_DEPLOYMENT.md            # Deployment guide
├── CLAUDE.md                    # Development guidelines
├── .env.example                 # Environment template
└── README.md                    # This file
```

### Synchronization Flow

**Manual Sync (Anytime):**
1. User/Scheduler makes HTTP request to `GET /sync?days=7`
2. App fetches transactions from Monobank
3. App analyzes with Gemini AI
4. Results saved to database
5. Report sent to Telegram

**Scheduled Sync (Daily at 8 AM UTC):**
- Fly Machines scheduler triggers `/sync` endpoint automatically
- No internal cron job needed
- Reliable even if main app restarts

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

### Building from Source
```bash
go build -o finance-bot main.go
```

### Running with Live Code Changes
```bash
go run main.go
```

### Environment Variables for Development
Create a `.env` file with test credentials (see `.env.example`).

### Testing Endpoints Locally
```bash
# Health check
curl http://localhost:8080/health

# Trigger sync
curl http://localhost:8080/sync?days=7

# Test Telegram webhook
curl -X POST http://localhost:8080/telegram/webhook \
  -H "Content-Type: application/json" \
  -d '{"update_id":1,"message":{"message_id":1,"text":"/status","chat":{"id":YOUR_CHAT_ID}}}'
```

### Code Conventions

- **Language**: English for all code, comments, error messages, and logs
- **Monetary Values**: Stored as `int64` integers representing kopiykas (e.g., 100 UAH = 10000 kopiykas)
  - Convert for display: `amount / 100` to get UAH
  - Positive values = income, negative values = expense
- **Error Handling**: Always wrap errors using `%w` to preserve context
- **No CGO**: Uses `modernc.org/sqlite` (pure Go driver) for cross-platform compatibility
- **API Limits**: Monobank statement requests are limited to 31 days per request; the app enforces this automatically

## Deployment

### Option 1: Local Use (Recommended for Personal Use)
Simply run the exe regularly:
```bash
.\finance-bot.exe  # Or schedule with Windows Task Scheduler / cron
```

### Option 2: Fly.io (Cloud Deployment)

For detailed instructions, see [FLY_DEPLOYMENT.md](FLY_DEPLOYMENT.md).

**Quick start:**
```bash
# Set environment variables
flyctl secrets set \
  MONOBANK_TOKEN=your_token \
  MONOBANK_ACCOUNT_ID=your_account \
  TELEGRAM_BOT_TOKEN=your_bot_token \
  TELEGRAM_CHAT_ID=your_chat_id \
  ANTHROPIC_API_KEY=your_key \
  SERVER=1

# Create volume for database persistence
flyctl volumes create finance_db --size 1

# Deploy with: flyctl deploy
```

**Auto-scheduling with Fly Machines:**
- Triggers `GET /sync?days=31` daily at 8 AM UTC
- Reliable even if main app restarts
- See [FLY_DEPLOYMENT.md](FLY_DEPLOYMENT.md) for complete setup


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

### Monthly Income (Optional but Recommended)
Set your monthly income for accurate budget projections:
```bash
# Via Telegram command (if running as server):
/setincome 15500000  # (amounts in kopiykas; 155,000 UAH in this example)

# Or directly in database:
sqlite3 finance-bot.db "INSERT OR REPLACE INTO settings VALUES('monthly_income', '15500000');"
```

## Future Roadmap

- [ ] Unit tests with mocks for all packages
- [ ] Populate and utilize the `monthly_stats` table for historical analysis
- [ ] Direct Telegram command processing (implement `/sync` and `/stats` as Telegram commands)
- [ ] Spending forecast (predict budget deficit dates)
- [ ] Multi-language support
- [ ] Mobile app integration (display charts, detailed analytics)
- [ ] Advanced filtering and search in transaction history

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

This project is provided as-is for personal use. Ensure you comply with Monobank, Telegram, and Google's terms of service when using this bot.

## Support

For issues, improvements, or questions, please open an issue in the repository.

---

**Built with Go** • **Powered by Monobank + Telegram + Google Gemini**
