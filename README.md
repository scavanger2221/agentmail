# agentmail

> Agent-first CLI email client. Built for AI agents — JSON output, SQLite cache, FTS5 search.

```
$ agentmail emails search "stepfun"   # 15ms across 34K emails
$ agentmail emails send               # pipe JSON, it sends
$ echo '{"to":["bob@example.com"],"subject":"yo","body":"hi"}' | agentmail emails send
```

## Install

```bash
git clone https://github.com/scavanger2221/agentmail.git
cd agentmail
make install PREFIX=$HOME/.local
```

Requires Go 1.21+ and `gcc` (for SQLite). If `~/.local/bin` isn't in your `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Agent Installation Guide (programmatic)

This section is for AI agents setting up agentmail themselves. Follow these steps in order.

### Prerequisites check

```bash
# Check Go
go version 2>/dev/null || { echo "Go required: https://go.dev"; exit 1; }

# Check gcc (for SQLite)
gcc --version 2>/dev/null || { echo "gcc required"; exit 1; }

# Check gh
gh auth status 2>/dev/null || { echo "gh CLI required and must be authenticated"; exit 1; }
```

### Step 1: Clone and build

```bash
git clone https://github.com/scavanger2221/agentmail.git /tmp/agentmail
cd /tmp/agentmail
make install PREFIX=$HOME/.local
export PATH="$HOME/.local/bin:$PATH"
```

### Step 2: Generate Gmail App Password

```bash
# Check if already have one
APP_PASSWORD=""

# Read from existing password file if it exists
if [ -f "$HOME/.config/agentmail/gmail-pass" ]; then
    APP_PASSWORD=$(cat "$HOME/.config/agentmail/gmail-pass")
fi

# If not found, instructions for human:
if [ -z "$APP_PASSWORD" ]; then
    echo "Need Gmail App Password. Ask the user to visit:"
    echo "  https://myaccount.google.com/apppasswords"
    echo "  App: Mail -> Other: agentmail -> Generate"
    echo "Then run: echo -n 'THE_CODE' > ~/.config/agentmail/gmail-pass"
    exit 1
fi
```

### Step 3: Create config

```bash
mkdir -p "$HOME/.config/agentmail"

NAME="${AGENTMAIL_ACCOUNT_NAME:-gmail}"
EMAIL="${AGENTMAIL_EMAIL}"

if [ -z "$EMAIL" ]; then
    # Try to read from existing config
    if [ -f "$HOME/.config/agentmail/config.toml" ]; then
        EMAIL=$(grep "^email" "$HOME/.config/agentmail/config.toml" | head -1 | cut -d'"' -f2)
    fi
fi

if [ -z "$EMAIL" ]; then
    echo "Set AGENTMAIL_EMAIL env var or configure manually"
    cat <<EOF > "$HOME/.config/agentmail/config.toml"
default_account = "$NAME"

[accounts.$NAME]
email = "REPLACE_WITH_YOUR_EMAIL@gmail.com"
imap_host = "imap.gmail.com"
imap_port = 993
smtp_host = "smtp.gmail.com"
smtp_port = 587
auth_method = "password"
password_file = "$HOME/.config/agentmail/gmail-pass"
EOF
    exit 1
fi

cat <<EOF > "$HOME/.config/agentmail/config.toml"
default_account = "$NAME"

[accounts.$NAME]
email = "$EMAIL"
imap_host = "imap.gmail.com"
imap_port = 993
smtp_host = "smtp.gmail.com"
smtp_port = 587
auth_method = "password"
password_file = "$HOME/.config/agentmail/gmail-pass"
EOF
```

### Step 4: Register and sync

```bash
agentmail accounts add "$NAME"
agentmail sync --bg

# Wait for sync to complete:
sleep 1
while agentmail cache status 2>&1 | grep -q '"emails": 0'; do
    echo "Waiting for sync..."
    sleep 3
done
```

### Step 5: Verify

```bash
agentmail cache status
agentmail emails list --limit 3
agentmail emails search "example"
```

### Expected state after setup

| File / Path | Purpose |
|---|---|
| `~/.config/agentmail/config.toml` | Account config |
| `~/.config/agentmail/gmail-pass` | App password |
| `~/.local/share/agentmail/cache.db` | SQLite cache + FTS5 index |
| `~/.local/bin/agentmail` | Binary |

### Error recovery

- **"Failed to open cache"**: Delete `~/.local/share/agentmail/cache.db` and re-run sync.
- **"Login failed"**: App password expired — regenerate at https://myaccount.google.com/apppasswords
- **"Account not found in cache"**: Run `agentmail accounts add gmail` first
- **Search returns no results**: Run `agentmail cache fetch-bodies --limit 5000` or `agentmail emails get <uid>` to cache body text

## Quick Start (Gmail)

### 1. Generate App Password

Go to https://myaccount.google.com/apppasswords → App: Mail, Device: agentmail → copy the 16-char code.

### 2. Config

```bash
mkdir -p ~/.config/agentmail
```

`~/.config/agentmail/config.toml`:

```toml
default_account = "gmail"

[accounts.gmail]
email = "you@gmail.com"
imap_host = "imap.gmail.com"
imap_port = 993
smtp_host = "smtp.gmail.com"
smtp_port = 587
auth_method = "password"
password_file = "~/.config/agentmail/gmail-pass"
```

### 3. Store password

```bash
echo -n "your-16-char-app-password" > ~/.config/agentmail/gmail-pass
chmod 600 ~/.config/agentmail/gmail-pass
```

### 4. Sync

```bash
agentmail accounts add gmail
agentmail sync --bg    # background, 34K emails in ~2 min
```

### 5. Use

```bash
agentmail emails list --limit 5
agentmail emails search "receipt"
agentmail cache status
```

## Commands

| Command | Description |
|---|---|
| `accounts list` | List configured accounts |
| `accounts add <name>` | Register account in cache |
| `folders list` | List IMAP mailboxes |
| `emails list` | List emails (`--folder`, `--limit`, `--offset`) |
| `emails get <uid>` | Fetch full email with body |
| `emails search <query>` | FTS5 search (subject, from, body) |
| `emails search --remote <query>` | IMAP server search (full body) |
| `emails send` | Send email (JSON on stdin) |
| `emails reply <uid>` | Reply to email |
| `emails trash <uid>` | Move to trash |
| `sync` / `sync --bg` | Sync IMAP → local cache |
| `sync --folders INBOX,Sent` | Sync specific folders |
| `cache status` | Cache statistics |
| `cache fetch-bodies --limit N` | Fetch body text for FTS5 |

## Output

Default: JSON envelope. Use `--format text` for human-readable tables.

```json
{
  "success": true,
  "data": {
    "emails": [
      {
        "uid": 34587,
        "subject": "Security alert",
        "from": "no-reply@accounts.google.com",
        "snippet": "Someone just signed in to your Google Account...",
        "date": "2026-06-08T08:46:40Z"
      }
    ],
    "total": 1
  },
  "meta": {
    "account": "you@gmail.com",
    "elapsed_ms": 15,
    "cached": true
  }
}
```

## Send Email

```bash
echo '{"to":["alice@example.com"],"cc":["bob@example.com"],"subject":"Hello","body":"This is a test"}' | agentmail emails send
```

## Search

Local (FTS5): instant, searches subject + from + body text.

```bash
agentmail emails search "invoice"
```

Remote (IMAP SEARCH): searches full message body on server.

```bash
agentmail emails search --remote "invoice"
```

Body text is cached when you `emails get <uid>` or run `cache fetch-bodies`.

## Multiple Accounts

```toml
[accounts.gmail]
email = "personal@gmail.com"
...

[accounts.work]
email = "me@company.com"
...

# Use --account flag:
agentmail --account work emails list
```

## OAuth2 (optional)

Set up Google Cloud OAuth credentials, then:

```bash
export AGENTMAIL_GMAIL_CLIENT_ID="..."
export AGENTMAIL_GMAIL_CLIENT_SECRET="..."
agentmail accounts add gmail   # opens browser, you sign in, done
```

## Cache

SQLite at `~/.local/share/agentmail/cache.db` with FTS5 full-text index. WAL mode for concurrent reads.

## Contributing

PRs welcome. `make build` to compile, `make test` to run tests.

## License

MIT
