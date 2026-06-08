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
