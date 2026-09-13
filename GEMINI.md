# Finance Bot (Фінансовий Радник)

This repository contains a Go-based personal finance advisor bot that synchronizes transactions from Monobank, stores them in an SQLite database, analyzes them using Google's Gemini LLM, and delivers structured financial reports and budget advice via Telegram.

---

## 🛠️ Project Overview & Architecture

### Key Technologies
- **Language:** Go (1.25.0+)
- **Database:** SQLite via `modernc.org/sqlite` (Pure Go driver, NO CGO required!)
- **APIs Integrated:**
  - **Monobank Personal API** (Retrieving statements & account info)
  - **Telegram Bot API** (Webhook handling and message broadcasting)
  - **Google Gemini API** (`gemini-1.5-flash-latest` model for financial analysis)

### Architecture
The project is modularized into distinct packages orchestrated by `main.go` using an `App` struct:
1. **`main.go` (App Orchestrator):** Coordinates syncing logic, manages HTTP endpoints (`/health`, `/sync`, `/telegram/webhook`), and handles server startup.
2. **`storage/` (Database Layer):** Manages SQLite connection and schema. Contains `transactions` and `monthly_stats` tables. Saves transactions safely using `ON CONFLICT DO NOTHING`.
3. **`monobank/` (Banking Client):** Fetches transactions from Monobank within specified time boundaries (safeguarded to not exceed the Monobank 31-day limit per request).
4. **`telegram/` (Notification Client):** Sends markdown reports to the specified personal chat using a Telegram Bot.
5. **`llm/` (AI Engine Client):** Communicates with the Google Gemini API to analyze transactions and draft optimization tips.

---

## 🚀 Building, Running & Testing

### 1. Configuration (Environment Variables)
Create a `.env` file in the root directory. You can copy the template from `.env.example`:
```bash
cp .env.example .env
```
Fill in the following variables:
- `PORT` (default: `8080`)
- `DB_PATH` (default: `finance-bot.db`)
- `MONOBANK_TOKEN` (your Monobank personal token from https://api.monobank.ua/)
- `MONOBANK_ACCOUNT_ID` (your Monobank account ID)
- `TELEGRAM_BOT_TOKEN` (from @BotFather)
- `TELEGRAM_CHAT_ID` (your Telegram user/chat ID)
- `GEMINI_API_KEY` (from Google AI Studio)

### 2. Building the Project
To compile the application:
- **Windows:**
  ```powershell
  go build -o finance-bot.exe main.go
  ```
- **Linux / macOS:**
  ```bash
  go build -o finance-bot main.go
  ```

### 3. Running the Project
To run the server locally:
```bash
# Windows
.\finance-bot.exe

# Linux / macOS
./finance-bot
```
The server will start on the configured port (default `8080`).

### 4. Testing
- **Run Tests:** Currently, there are no tests implemented.
- **TODO:** Implement unit and integration tests under respective packages (`storage`, `monobank`, etc.).
- Run standard Go tests with:
  ```bash
  go test ./... -v
  ```

### 5. HTTP Endpoints
- `GET /health` - Liveness/readiness check (e.g., for Fly.io). Returns `OK` with HTTP 200.
- `GET /sync` - Triggers transaction synchronization, saves new transactions to SQLite, generates AI advice, and sends it to Telegram.
- `POST /telegram/webhook` - Handles updates sent from Telegram webhook (e.g., user messages/commands).

---

## 🎨 Development Conventions

### General Conventions
- **Code Language:** The code uses English for variable names and package structures, but code comments, logs, and user-facing messages (Telegram / Gemini prompt instructions) are in **Ukrainian**. Maintain this standard.
- **Error Handling:** Always wrap errors using `%w` to preserve context, e.g., `fmt.Errorf("failed to do X: %w", err)`.
- **Database Rules:** 
  - Monetary values are stored as **integers representing cents/kopiykas** (e.g., 100 UAH is represented as `10000`).
  - Positive values denote `income`, negative values denote `expense`.
  - Avoid using CGO. Stick to `modernc.org/sqlite` to ensure the project remains cross-compilable without any native dependency issues.
- **API Limits Compliance:** Monobank statement requests are restricted to a maximum of 31 days per request. The app handles this safely in `main.go`.

### Future Roadmap (TODOs)
- **Telegram Bot Commands:** Finish implementing the command processor (`processCommand`) in `main.go` to support `/sync` and `/stats` commands directly from Telegram chat instead of calling the HTTP endpoint.
- **Monthly Statistics:** Populate and utilize the `monthly_stats` table defined in `storage/sqlite.go`.
- **Unit Testing:** Implement mock-based unit tests for API clients (`telegram`, `monobank`, `llm`).
