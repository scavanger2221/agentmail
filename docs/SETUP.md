# agentmail Setup Guide

## Prerequisites

- Go 1.21+ (to build from source)
- A Gmail or Outlook account
- Either an App Password or OAuth2 credentials

## Installation

```bash
cd ~/Project/agentmail
make build           # builds ./agentmail locally
make install         # installs to /usr/local/bin (may need sudo)
make install PREFIX=~/.local   # install to ~/.local/bin (no sudo needed)
```

After `make install`, `agentmail` is globally available as a command.

## Quick Start (Gmail with App Password)

### Step 1: Build & Install

```bash
cd ~/Project/agentmail
make install PREFIX=$HOME/.local
# Ensure ~/.local/bin is in your PATH:
export PATH="$HOME/.local/bin:$PATH"
```

### Step 2: Generate Gmail App Password

1. Go to https://myaccount.google.com/apppasswords
2. Sign in with your Gmail account
3. Select **App** = "Mail", **Device** = "Other (agentmail)"
4. Google shows a 16-character password like `abcd efgh ijkl mnop`
5. Copy it (spaces don't matter, keep them or remove them)

### Step 3: Create config

```bash
mkdir -p ~/.config/agentmail
```

Write `~/.config/agentmail/config.toml`:

```toml
default_account = "gmail"

[accounts.gmail]
email = "your.email@gmail.com"
imap_host = "imap.gmail.com"
imap_port = 993
smtp_host = "smtp.gmail.com"
smtp_port = 587
auth_method = "password"
password_file = "~/.config/agentmail/gmail-pass"
```

### Step 4: Store password

```bash
echo -n "abcd efgh ijkl mnop" > ~/.config/agentmail/gmail-pass
chmod 600 ~/.config/agentmail/gmail-pass
```

Or use an environment variable:

```bash
export AGENTMAIL_PASSWORD="abcd efgh ijkl mnop"
```

Add that line to `~/.bashrc` or `~/.zshrc` to persist it.

### Step 5: Add account to cache

```bash
./agentmail accounts add gmail
```

### Step 6: Sync emails

```bash
./agentmail sync
```

### Step 7: Test

```bash
./agentmail emails list --limit 5
./agentmail emails search "meeting"
./agentmail cache status
```

---

## Quick Start (Outlook / Office 365)

Same steps but config:

```toml
default_account = "outlook"

[accounts.outlook]
email = "you@outlook.com"
imap_host = "outlook.office365.com"
imap_port = 993
smtp_host = "smtp.office365.com"
smtp_port = 587
auth_method = "password"
password_file = "~/.config/agentmail/outlook-pass"
```

Outlook also requires an App Password (go to https://account.microsoft.com/security → Security → Advanced security options → App passwords).

---

## OAuth2 Setup (Gmail) — Simple Browser Flow

`agentmail` opens your browser automatically — you just sign in, no copy-paste needed.

### Step 1: Create Google Cloud OAuth credentials

1. Go to https://console.cloud.google.com
2. Create a new project (or use existing)
3. **Enable the Gmail API** (APIs & Services → Library → search "Gmail API" → Enable)
4. **Configure OAuth consent screen** (APIs & Services → OAuth consent screen):
   - User type: **External**
   - App name: `agentmail`
   - Contact email: your email
   - Scopes: add `https://mail.google.com/`
   - Add yourself as a test user
5. **Create OAuth client ID** (APIs & Services → Credentials → Create Credentials → OAuth client ID):
   - Application type: **Desktop application**
   - Add `http://127.0.0.1` to Authorized redirect URIs
   - Click Create
6. Copy the Client ID and Client Secret

### Step 2: Set credentials

```bash
export AGENTMAIL_GMAIL_CLIENT_ID="123456789-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx.apps.googleusercontent.com"
export AGENTMAIL_GMAIL_CLIENT_SECRET="GOCSPX-xxxxxxxxxxxxxxxxxxxx"
```

### Step 3: Set up config

```toml
[accounts.gmail]
email = "you@gmail.com"
imap_host = "imap.gmail.com"
imap_port = 993
smtp_host = "smtp.gmail.com"
smtp_port = 587
auth_method = "oauth2"
```

### Step 4: Authorize

```bash
agentmail accounts add gmail
```

Your browser opens. Sign in with Google. That's it — no codes to copy.

---

## Command Reference

| Command | Purpose | Example |
|---|---|---|
| `accounts list` | List configured accounts | `agentmail accounts list` |
| `accounts add <name>` | Register account in local cache | `agentmail accounts add gmail` |
| `accounts remove <name>` | Remove account | `agentmail accounts remove gmail` |
| `folders list` | List IMAP mailboxes | `agentmail folders list` |
| `emails list` | List emails in a folder | `agentmail emails list --limit 10 --folder INBOX` |
| `emails get <uid>` | Fetch full email with body | `agentmail emails get 42` |
| `emails search <query>` | Search local cache | `agentmail emails search "receipt"` |
| `emails send` | Send email (JSON on stdin) | See below |
| `emails reply <uid>` | Reply to email | See below |
| `emails trash <uid>` | Move to trash | `agentmail emails trash 42` |
| `sync` | Force sync IMAP → cache | `agentmail sync` |
| `cache status` | Cache statistics | `agentmail cache status` |

### Send an email

```bash
echo '{"to":["alice@example.com"],"subject":"Hello","body":"This is a test"}' | ./agentmail emails send
```

### Reply to an email

```bash
echo '{"body":"Thanks for the update!"}' | ./agentmail emails reply 142
```

---

## Global Flags

| Flag | Default | Description |
|---|---|---|
| `--config` | `~/.config/agentmail/config.toml` | Config file path |
| `--format` | `json` | Output format: `json` or `text` |
| `--account` | First configured | Which account to use |

---

## Files & Paths

| Path | Purpose |
|---|---|
| `~/.config/agentmail/config.toml` | Config file (accounts, servers) |
| `~/.config/agentmail/tokens/<name>.json` | OAuth2 tokens |
| `~/.local/share/agentmail/cache.db` | SQLite email cache |
| `~/.local/share/agentmail/data/` | Cached email bodies & attachments |

---

## Troubleshooting

### "No password configured"
Set `AGENTMAIL_PASSWORD` env var or configure `password_file` in `config.toml`.

### "Failed to connect / TLS dial"
- Check IMAP host and port are correct
- Gmail: `imap.gmail.com:993`
- Ensure IMAP is enabled in Gmail settings (Settings → See all settings → Forwarding and POP/IMAP → Enable IMAP)

### "Login failed / AUTH_FAILED"
- For Gmail: use an App Password, NOT your normal Google password
- For Outlook: use an App Password from Microsoft account security
- Check 2FA isn't blocking you (App Passwords work with 2FA)

### "Account not found in cache"
Run `./agentmail accounts add <name>` first, then `./agentmail sync`.

### "No emails in cache"
Run `./agentmail sync` to populate the cache. Or run `./agentmail emails list` which will auto-sync on first use.

### Search returns no results
Search runs against the local SQLite cache. Run `./agentmail sync` to update the cache first.

### Config file not found
The default path is `~/.config/agentmail/config.toml`. Create the directory and file if they don't exist.

### OAuth2 token expired
Re-authenticate by removing and re-adding the account, or run `./agentmail accounts remove gmail && ./agentmail accounts add gmail`.

---

## Multiple Accounts

```toml
default_account = "gmail"

[accounts.gmail]
email = "personal@gmail.com"
imap_host = "imap.gmail.com"
imap_port = 993
smtp_host = "smtp.gmail.com"
smtp_port = 587
auth_method = "password"
password_file = "~/.config/agentmail/gmail-pass"

[accounts.work]
email = "me@company.com"
imap_host = "imap.company.com"
imap_port = 993
smtp_host = "smtp.company.com"
smtp_port = 587
auth_method = "password"
password_file = "~/.config/agentmail/work-pass"
```

Use `--account work` to operate on the work account:

```bash
./agentmail --account work emails list
```
