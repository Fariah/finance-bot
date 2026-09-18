# Fly.io Deployment Guide

This guide explains how to deploy finance-bot on Fly.io with scheduled daily synchronization using Fly Machines.

## Architecture

- **App Service**: HTTP server that handles requests (`/health`, `/sync` endpoints)
- **Scheduler Machine**: Separate Fly Machine that runs on a schedule and triggers the `/sync` endpoint

This separation ensures:
- ✅ Reliable scheduling (not dependent on app restarts)
- ✅ Clean separation of concerns
- ✅ Easy monitoring and debugging

## Prerequisites

1. [Fly.io account](https://fly.io) with at least $5/month credits
2. [Flyctl CLI](https://fly.io/docs/hands-on/install-flyctl/) installed
3. All API credentials ready:
   - Monobank personal token
   - Monobank account ID
   - Telegram bot token
   - Telegram chat ID
   - Google Gemini API key

## Deployment Steps

### 1. Create Fly App

```bash
cd finance-bot
flyctl launch
```

Answer the prompts:
- App name: `finance-bot` (or your preferred name)
- Region: Choose closest to you (e.g., `ams` for Europe, `sjc` for US)
- PostgreSQL: **No** (we use SQLite)
- Redis: **No**

### 2. Set Environment Variables & Secrets

```bash
# Set all required secrets
flyctl secrets set \
  MONOBANK_TOKEN="your_monobank_token_here" \
  MONOBANK_ACCOUNT_ID="your_account_id_here" \
  TELEGRAM_BOT_TOKEN="your_telegram_bot_token_here" \
  TELEGRAM_CHAT_ID="your_telegram_chat_id_here" \
  GEMINI_API_KEY="your_gemini_api_key_here"
```

Verify secrets were set:
```bash
flyctl secrets list
```

### 3. Create Persistent Volume for SQLite Database

This ensures your database persists across app restarts:

```bash
# Create a 1GB volume
flyctl volumes create finance_db --size 1
```

Then uncomment the `[mounts]` section in `fly.toml`:

```toml
[[mounts]]
  source = "finance_db"
  destination = "/data"
  processes = ["app"]
```

And update environment variable in your app:
```bash
flyctl secrets set DB_PATH="/data/finance-bot.db"
```

### 4. Deploy the App

```bash
flyctl deploy
```

This will:
- Build the Go application
- Push it to Fly.io
- Start the app service
- Monitor the deployment

Verify deployment:
```bash
flyctl status
```

Test health endpoint:
```bash
curl https://your-app-name.fly.dev/health
```

### 5. Set Up Scheduled Sync with Fly Machines

Fly Machines is Fly.io's serverless compute service that can run on a schedule. We'll use it to trigger `/sync` daily.

#### Option A: Using Fly Machines Scheduler (Recommended)

Create a machine that runs daily at 8 AM UTC:

```bash
flyctl machines create \
  --region ams \
  --image flyio/hellofly:latest \
  --name finance-bot-scheduler \
  --schedule daily \
  --schedule-value "08:00" \
  --env SYNC_URL="https://your-app-name.fly.dev/sync?days=7"
```

**Note:** The above creates a basic machine. For a better approach, we'll create a custom scheduler machine:

#### Option B: Create Custom Scheduler Script (Preferred)

Create a `scheduler/Dockerfile`:

```dockerfile
FROM curlimages/curl:latest

# Simple script that triggers the sync endpoint
ENTRYPOINT ["/bin/sh", "-c"]
CMD ["curl -X GET ${SYNC_URL} && echo 'Sync triggered successfully'"]
```

Deploy it as a machine:

```bash
flyctl machine create \
  --name finance-bot-scheduler \
  --region ams \
  --schedule daily \
  --schedule-value "08:00" \
  --env SYNC_URL="https://your-app-name.fly.dev/sync?days=7" \
  scheduler/Dockerfile
```

#### Option C: Using GitHub Actions (Alternative)

If your code is on GitHub, use GitHub Actions for scheduling:

Create `.github/workflows/daily-sync.yml`:

```yaml
name: Daily Sync

on:
  schedule:
    - cron: '0 8 * * *'  # 8 AM UTC daily

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger daily sync
        run: |
          curl -X GET "https://your-app-name.fly.dev/sync?days=7" \
            -H "User-Agent: finance-bot-scheduler"
```

### 6. Verify Everything Works

#### Test Manual Sync

```bash
# Trigger sync manually
curl "https://your-app-name.fly.dev/sync?days=7"
```

#### Check Logs

```bash
# View app logs
flyctl logs

# View machine logs (if using scheduler machine)
flyctl logs --machine <machine-id>
```

#### Monitor Deployment

```bash
# Check app status
flyctl status

# Check volumes
flyctl volumes list
```

## Database Persistence

### Without Volume (temporary database)

```bash
# Database is stored in app container (lost on restarts)
flyctl secrets set DB_PATH="finance-bot.db"
```

### With Volume (recommended)

```bash
# Create volume
flyctl volumes create finance_db --size 1

# Mount in fly.toml and set path
flyctl secrets set DB_PATH="/data/finance-bot.db"
```

## Scaling & Costs

- **App Service**: 1 shared-cpu-1x 256MB = ~$1.70/month
- **Scheduler Machine**: ~$0.50/month (runs once daily)
- **Volume**: 1GB = $0.10/month
- **Total**: ~$2.30/month (within free tier if you have credits)

To scale up:
```bash
flyctl machine update <machine-id> --cpus 2 --memory 512
```

## Troubleshooting

### App won't start

```bash
# Check logs
flyctl logs

# Check configuration
flyctl config show
```

### Database errors

```bash
# Check volume status
flyctl volumes list
flyctl volumes show <volume-id>

# Recreate volume if corrupted
flyctl volumes delete <volume-id>
flyctl volumes create finance_db --size 1
```

### Scheduler not running

```bash
# List machines
flyctl machines list

# Check machine status
flyctl machine show <machine-id>

# View machine logs
flyctl logs --machine <machine-id>
```

### SSL/HTTPS issues

```bash
# Flyctl handles SSL automatically, but if issues:
flyctl certs check
```

## Updating the App

```bash
# Make code changes locally
git add .
git commit -m "Your changes"

# Deploy
flyctl deploy

# Monitor deployment
flyctl status
flyctl logs
```

## Backup & Recovery

### Backup Database

```bash
# Download database file
flyctl ssh console
tar czf finance-bot.tar.gz /data/finance-bot.db
exit

# Then download from volume
flyctl volumes download <volume-id> finance-bot.tar.gz
```

### Restore from Backup

```bash
# Upload to new volume
flyctl volumes upload <volume-id> finance-bot.tar.gz /data/
```

## Security Best Practices

1. **Secrets**: Always use `flyctl secrets set`, never hardcode
2. **HTTPS**: Fly.io enforces HTTPS automatically
3. **Network**: App is not publicly accessible to Telegram webhook (good!)
4. **Backups**: Regularly backup your database volume

## Useful Commands

```bash
# View current app status
flyctl status

# SSH into running instance
flyctl ssh console

# View environment variables
flyctl secrets list

# Update a secret
flyctl secrets set VAR_NAME="new_value"

# Scale app
flyctl scale count 2  # Run 2 instances

# Monitor real-time logs
flyctl logs --follow

# View metrics
flyctl metrics
```

## Advanced: Using Fly Cron Machines (Beta)

If you have access to Fly's Cron Machines beta:

```bash
flyctl machines create \
  --image curlimages/curl \
  --region ams \
  --schedule-cron "0 8 * * *" \
  --cmd "curl -X GET https://your-app-name.fly.dev/sync?days=7"
```

## Support & Debugging

For issues:

1. Check logs: `flyctl logs`
2. Check Fly.io dashboard: https://fly.io/dashboard
3. Check health endpoint: `curl https://your-app-name.fly.dev/health`
4. Review [Fly.io documentation](https://fly.io/docs/)

---

**Deployment successful!** Your finance-bot is now:
- ✅ Running on Fly.io with 99.9% uptime SLA
- ✅ Syncing Monobank data daily at 8 AM UTC
- ✅ Accessible to Telegram commands
- ✅ Persisting data in a reliable volume
