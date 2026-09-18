# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Architecture Overview

The finance-bot is a Go-based personal finance advisor that synchronizes bank transactions, stores them in SQLite, analyzes them with Google Gemini LLM, and delivers reports via Telegram.

### Core Design Pattern

The entire app is orchestrated by the `App` struct in `main.go`, which composes four client modules:
- **App.storage** (`storage/`) - SQLite database layer managing transactions, obligations, and planned expenses
- **App.mono** (`monobank/`) - HTTP client for fetching bank statements from Monobank API
- **App.llm** (`llm/`) - HTTP client for sending transaction data to Google Gemini for analysis
- **App.tg** (`telegram/`) - HTTP client for sending formatted reports back to Telegram

The app runs as a stateless HTTP server with two main endpoints:
- `GET /health` - liveness check for deployment health monitoring
- `GET /sync` - triggers the synchronization pipeline (fetch → analyze → report)

### Key Architectural Constraints

- **No CGO:** Uses `modernc.org/sqlite` (pure Go driver) to avoid native dependencies and ensure cross-platform compilation.
- **Monetary representation:** All money amounts are stored as `int64` integers representing kopiykas (cents). Positive = income, negative = expense. Convert to UAH for display with `/ 100`.
- **Monobank API limit:** Statement requests cannot exceed 31 days. The `Sync` method in `main.go` enforces this with `thirtyOneDaysAgo` clamping.
- **Stateless HTTP:** The app stores persistent state in SQLite only; HTTP requests are independent.

### Data Model

**transactions** table: One row per bank transaction (ID, amount in kopiykas, description, MCC code, timestamp, type).

**obligations** table: Recurring mandatory payments (bills, transfers, subscriptions). Each obligation stores next_due_date and interval_months, allowing automatic forward-rolling (see `rollForwardObligations`).

**planned_expenses** table: One-time expenses the user considers (gifts, repairs). Status is either "pending" (awaiting approval), "approved" (included in budget), or "rejected" (discarded).

**settings** table: Key-value pairs for user configuration (e.g., monthly_income in kopiykas).

**monthly_stats** table: Defined but currently unused; reserved for future monthly aggregation.

## Common Development Tasks

### Build

```bash
# Windows
go build -o finance-bot.exe main.go

# Linux / macOS
go build -o finance-bot main.go
```

### Run Locally

```bash
# Requires .env file with environment variables (see Configuration)
go run main.go
```

The server listens on `$PORT` (default 8080).

### Configuration

Create a `.env` file in the project root (or copy from `.env.example`):
```
PORT=8080
DB_PATH=finance-bot.db
MONOBANK_TOKEN=<your-monobank-personal-token>
MONOBANK_ACCOUNT_ID=<your-account-id-from-monobank>
TELEGRAM_BOT_TOKEN=<your-bot-token-from-botfather>
TELEGRAM_CHAT_ID=<your-telegram-user-id>
GEMINI_API_KEY=<your-google-ai-studio-key>
```

Without all these variables, the app starts in demo mode (logs a warning, serves /health endpoint only).

### Testing

No test suite yet. Future work: add unit tests in `storage_test.go`, `monobank_test.go`, etc., using mock HTTP clients and in-memory SQLite databases.

To manually test endpoints:
```bash
# Test health check
curl http://localhost:8080/health

# Trigger sync (requires valid config)
curl http://localhost:8080/sync?days=7

# Test Telegram webhook (must match configured chat ID)
curl -X POST http://localhost:8080/telegram/webhook \
  -H "Content-Type: application/json" \
  -d '{"update_id":1,"message":{"message_id":1,"text":"/status","chat":{"id":YOUR_CHAT_ID}}}'
```

## Code Patterns and Conventions

### Error Messages

All error messages in code are in English. Wrap errors with `%w` to preserve context: `fmt.Errorf("failed to read setting: %w", err)`.

### Telegram Command Processing

New Telegram commands are added to the `processCommand` switch statement in `main.go`. Each case:
1. Parses arguments from the user's text
2. Validates input (amounts, dates, IDs)
3. Calls storage or API methods
4. Sends a formatted response via `app.tg.SendMessage()`

Examples: `/setincome`, `/addobligation`, `/addplanned`, `/confirmplanned`.

### Budget Calculation

`CalculateBudgetStatus` computes the remaining budget for the month by:
1. Fetching monthly income from settings
2. Summing obligations due this month (with automatic date roll-forward)
3. Summing spent transactions (expenses only) from month start to now
4. Subtracting approved planned expenses
5. Computing suggested daily limit based on days left

This single struct is reused in both the `/status` command and the AI analysis prompt.

### AI Analysis Prompt

`buildAnalysisPrompt` assembles a markdown-formatted prompt that includes:
- Current budget context (income, obligations, spent, remaining)
- List of obligations due this month
- List of pending planned expenses awaiting user decision
- The new transactions with MCC-derived categories

The prompt instructs Gemini to provide brief, actionable advice in English. Telegram's markdown parser then renders bold text and lists.

### MCC Categorization

`monobank/mcc.go` maps MCC (Merchant Category Code) codes to human-readable categories (Groceries, Restaurants, Transport, etc.). If a code is unknown, `CategoryForMCC` returns "Other".

## Deployment Architecture

The app uses **Fly.io** with **Fly Machines scheduler** for reliable daily synchronization:

1. **Main App Service** - Stateless HTTP server running 24/7, responds to `/sync` requests
2. **Scheduler Machine** - Separate Fly Machine that runs daily at 8 AM UTC, makes HTTP request to `/sync` endpoint
3. **Database Volume** - Persistent SQLite storage that survives app restarts

This approach is better than internal cron because:
- ✅ Scheduler runs independently (not affected by app restarts)
- ✅ Easy to monitor and debug
- ✅ Can run multiple sync schedules if needed
- ✅ Cheap ($0.50/month for scheduler machine)

See [FLY_DEPLOYMENT.md](FLY_DEPLOYMENT.md) for detailed setup instructions.

## Important Notes

- **Language:** All code comments, error messages, and strings use English.
- **No Internal Cron:** Scheduling happens via Fly Machines, not internal cron job
- **HTTP-Driven Sync:** Sync is triggered by HTTP requests, making it flexible (manual trigger, scheduled trigger, or external webhooks)
- **Database:** SQLite with optional persistent volume on Fly.io

## Future TODOs

- [ ] Populate and use the monthly_stats table for historical analysis
- [ ] Add unit tests with mocks for all packages
- [ ] Implement budget forecast (e.g., "at current spend rate, deficit by day X")
- [ ] Add support for multiple Monobank accounts
- [ ] Web dashboard for viewing reports (instead of Telegram-only)
