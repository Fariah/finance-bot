# Finance Bot - Personal Financial Advisor

A Go-based Telegram bot that analyzes your bank transactions in real-time, tracks your budget, and provides AI-powered financial advice. It syncs transactions from Monobank, stores them in SQLite, and leverages Google's Gemini AI to deliver actionable insights directly to Telegram.

## Features

- ✅ **Automatic Transaction Sync** - Fetches new transactions from Monobank API with automatic deduplication
- 📊 **Smart Budget Tracking** - Monitors income, mandatory payments, planned expenses, and remaining balance
- 🤖 **AI-Powered Analysis** - Uses Google Gemini to analyze spending patterns and provide personalized advice
- 💬 **Telegram Integration** - Receive reports and manage your budget via Telegram commands
- 🏦 **Multi-Account Ready** - Can be configured for different Monobank accounts and Telegram chats
- 📅 **Recurring Payment Management** - Track bills, transfers, and subscriptions with automatic date rolling
- 🎁 **Expense Planning** - Propose one-time purchases and get AI verdict on affordability
- 🏥 **MCC-Based Categorization** - Automatically categorizes spending by merchant type (Groceries, Restaurants, Transport, etc.)

## Quick Start

### Prerequisites

- Go 1.20 or higher
- Monobank account with personal API access
- Telegram Bot (create via @BotFather)
- Google AI Studio API key

### Installation

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd finance-bot
   ```

2. **Create environment configuration**
   ```bash
   cp .env.example .env
   ```

3. **Fill in your credentials in `.env`**
   ```env
   PORT=8080
   DB_PATH=finance-bot.db
   MONOBANK_TOKEN=your_monobank_token_here
   MONOBANK_ACCOUNT_ID=your_account_id_here
   TELEGRAM_BOT_TOKEN=your_telegram_bot_token_here
   TELEGRAM_CHAT_ID=your_telegram_user_id_here
   GEMINI_API_KEY=your_gemini_api_key_here
   ```

4. **Build the application**
   ```bash
   # Windows
   go build -o finance-bot.exe main.go
   
   # Linux / macOS
   go build -o finance-bot main.go
   ```

5. **Run the server**
   ```bash
   # Linux / macOS
   ./finance-bot
   
   # Windows
   .\finance-bot.exe
   ```

   The server will start on the configured port (default: 8080).

## API Endpoints

### Health Check
```
GET /health
```
Returns `OK` with HTTP 200. Used for deployment health monitoring (e.g., Fly.io).

### Manual Sync
```
GET /sync?days=7
```
Triggers transaction synchronization:
1. Fetches new transactions from Monobank for the last N days
2. Saves them to SQLite (with automatic deduplication)
3. Analyzes transactions with Google Gemini
4. Sends formatted report to Telegram

**Query Parameters:**
- `days` (optional): Number of days to fetch (default: 7). Note: Monobank API is limited to 31-day windows.

**Response:**
- `200 OK`: "Synchronization successful! Report sent to Telegram."
- `503 Service Unavailable`: Bot not initialized (missing environment variables)
- `400 Bad Request`: Invalid `days` parameter

### Telegram Webhook
```
POST /telegram/webhook
```
Receives updates from Telegram webhook. Processes user commands and updates database state.

Expects JSON in Telegram webhook format:
```json
{
  "update_id": 123,
  "message": {
    "message_id": 1,
    "text": "/setincome 45000",
    "chat": {
      "id": 123456789
    }
  }
}
```

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
- Sends structured prompts to Google Gemini API
- Receives AI-generated financial analysis
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

### Fly.io (Recommended)

For detailed deployment instructions, see [FLY_DEPLOYMENT.md](FLY_DEPLOYMENT.md).

**Quick start:**

```bash
# Launch app on Fly.io
flyctl launch

# Set secrets
flyctl secrets set \
  MONOBANK_TOKEN=your_token \
  MONOBANK_ACCOUNT_ID=your_account \
  TELEGRAM_BOT_TOKEN=your_bot_token \
  TELEGRAM_CHAT_ID=your_chat_id \
  GEMINI_API_KEY=your_key

# Create persistent volume for SQLite database
flyctl volumes create finance_db --size 1

# Deploy
flyctl deploy
```

**Automatic scheduling:**
- Uses Fly Machines to trigger `/sync` endpoint daily at 8 AM UTC
- No internal cron job needed
- Reliable even if app restarts
- See [FLY_DEPLOYMENT.md](FLY_DEPLOYMENT.md#5-set-up-scheduled-sync-with-fly-machines) for setup details


## Configuration

### Monobank API
1. Get your personal token from https://api.monobank.ua/
2. Find your account ID by calling the `/personal/client-info` endpoint or from the API documentation
3. Set `MONOBANK_TOKEN` and `MONOBANK_ACCOUNT_ID` in your `.env` file

### Telegram Bot
1. Create a bot via [@BotFather](https://t.me/botfather) on Telegram
2. Set up a webhook pointing to your server's `/telegram/webhook` endpoint
3. Get your user/chat ID (send `/start` to your bot and check the chat ID in the webhook)
4. Set `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` in your `.env` file

### Google Gemini API
1. Get your API key from [Google AI Studio](https://aistudio.google.com/app/apikey)
2. Set `GEMINI_API_KEY` in your `.env` file

## Future Roadmap

- [ ] Unit tests with mocks for all packages
- [ ] Populate and utilize the `monthly_stats` table for historical analysis
- [ ] Direct Telegram command processing (implement `/sync` and `/stats` as Telegram commands)
- [ ] Spending forecast (predict budget deficit dates)
- [ ] Multi-language support
- [ ] Mobile app integration (display charts, detailed analytics)
- [ ] Advanced filtering and search in transaction history

## Troubleshooting

**"Bot started in limited demo mode"**
- Some environment variables are missing. Check that all required variables are set in `.env` and sourced into the process.

**"Monobank returned status: 429"**
- Rate limited. Monobank API has request limits. Wait a few seconds before retrying.

**"Empty response from LLM"**
- Google Gemini API request failed. Check your API key and ensure you have quota remaining.

**"No new periods to synchronize"**
- Less than 10 seconds have elapsed since the last sync. Monobank doesn't return transactions with sub-second precision.

## License

This project is provided as-is for personal use. Ensure you comply with Monobank, Telegram, and Google's terms of service when using this bot.

## Support

For issues, improvements, or questions, please open an issue in the repository.

---

**Built with Go** • **Powered by Monobank + Telegram + Google Gemini**
