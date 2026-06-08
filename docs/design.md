# agentmail — Agent-First Email CLI

## Overview

`agentmail` is a Go CLI email client designed for AI agent consumption. It outputs
structured JSON by default, uses a local SQLite cache for fast search and listing,
and supports IMAP + SMTP with OAuth2.

## Commands

```
agentmail [--config FILE] [--format json|text] <command> [<args>]

Config & accounts:
  accounts list
  accounts add <name>
  accounts remove <name>

Folders:
  folders list [--account <name>]

Emails:
  emails list   [--account <name>] [--folder INBOX] [--limit 20] [--offset 0]
  emails get    <uid>
  emails search <query>
  emails send                    # reads JSON message from stdin
  emails reply  <uid>            # reads body from stdin
  emails trash  <uid>

Cache:
  sync          [--account <name>]
  cache status
```

### Global flags
- `--config` — path to config file (default `~/.config/agentmail/config.toml`)
- `--format json|text` — output format (default `json`)
- `--account` — specify account (default: first configured)

## SQLite Schema

Location: `~/.local/share/agentmail/cache.db` (WAL mode)

### `accounts`
| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| name | TEXT UNIQUE | short nickname |
| email | TEXT | user@example.com |
| imap_host | TEXT | imap.gmail.com |
| imap_port | INTEGER | 993 |
| smtp_host | TEXT | smtp.gmail.com |
| smtp_port | INTEGER | 587 |
| auth_method | TEXT | "oauth2" or "password" |
| oauth_token | TEXT | encrypted OAuth2 token JSON |
| password_file | TEXT | path to app-password file |

### `folders`
| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| account_id | INTEGER FK | |
| name | TEXT | "INBOX", "Sent" |
| uid_validity | INTEGER | IMAP UIDVALIDITY |
| last_sync_uid | INTEGER | highest synced UID |

### `emails`
| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| account_id | INTEGER FK | |
| folder_id | INTEGER FK | |
| uid | INTEGER | IMAP UID |
| message_id | TEXT | Message-ID header |
| subject | TEXT | |
| from_addr | TEXT | |
| to_addrs | TEXT | comma-separated |
| date | TEXT | RFC3339 |
| flags | TEXT | "\\Seen \\Flagged" |
| body_snippet | TEXT | first ~200 chars of plaintext |
| body_path | TEXT | path to cached full body file |
| size | INTEGER | RFC822 size |
| has_attachments | INTEGER | boolean |
| internal_date | TEXT | |
| is_synced | INTEGER | 0=metadata, 1=full body |
| last_sync_at | TEXT | timestamp of last sync |

### `attachments`
| Column | Type | Notes |
|---|---|---|
| id | INTEGER PK | |
| email_id | INTEGER FK | |
| filename | TEXT | |
| mime_type | TEXT | |
| size | INTEGER | |
| storage_path | TEXT | path to cached binary |

## Config File

Default location: `~/.config/agentmail/config.toml`

```toml
# Default account to use when --account is omitted
default_account = "gmail"

[accounts.gmail]
email = "user@gmail.com"
imap_host = "imap.gmail.com"
imap_port = 993
smtp_host = "smtp.gmail.com"
smtp_port = 587
auth_method = "oauth2"
# oauth_token is stored separately (encrypted) by accounts add

[accounts.outlook]
email = "user@outlook.com"
imap_host = "outlook.office365.com"
imap_port = 993
smtp_host = "smtp.office365.com"
smtp_port = 587
auth_method = "password"
password_file = "~/.config/agentmail/outlook-pass"
```

## OAuth2

- Uses **device code flow** (RFC 8628) for initial setup — prints URL, user authenticates, CLI polls for token.
- Supports pre-configured refresh tokens in config for agent scenarios.
- Tokens encrypted at rest with AES-256-GCM (key derived from machine secret).
- Gmail: scopes `https://mail.google.com/`
- Outlook: scopes `https://outlook.office.com/IMAP.AccessAsUser.All` + SMTP.Send

## Search

`emails search <query>` performs a case-insensitive SQL `LIKE` search across
`subject` and `body_snippet` columns in the local SQLite cache. No network call.
Accepts `--remote` flag to delegate search to the IMAP server's SEARCH command instead.

## Sync Strategy

- **`emails list`:** If folder stale (UIDVALIDITY unchanged), fetch new UIDs via `UID SEARCH`.
  If UIDVALIDITY changed, full resync. Metadata only (subject, from, date, flags).
- **`emails get <uid>`:** If `body_path` empty, fetch full RFC822 from IMAP, extract body +
  attachments, cache to `~/.local/share/agentmail/bodies/<uid>/`. All attachments are
  downloaded automatically on `get`.
- **`emails search`:** Pure SQLite query as described above — instant, no network.
- **`sync`:** Force full crawl of all folders for an account.
- **`cache status`:** Show email count per folder, last sync time, total disk usage.
- **Thread safety:** SQLite in WAL mode allows concurrent readers. Writes are serialized
  via a single writer. The CLI is single-process per invocation, so no contention within
  one call, but multiple concurrent `agentmail` invocations are safe.

## Output Format

Default: JSON envelope.

```json
{
  "success": true,
  "data": { ... },
  "meta": {
    "account": "gmail",
    "elapsed_ms": 234,
    "cached": true
  }
}
```

Error:
```json
{
  "success": false,
  "error": {
    "code": "AUTH_FAILED",
    "message": "OAuth2 token expired."
  }
}
```

Error codes: `AUTH_FAILED`, `NOT_FOUND`, `IMAP_ERROR`, `SMTP_ERROR`, `CONFIG_ERROR`, `INTERNAL_ERROR`.

## Text Output

With `--format text`, list commands render aligned tables:
```
  UID  |  SUBJECT              |  FROM                  |  DATE
───────┼───────────────────────┼────────────────────────┼──────────────────────
  142  |  Meeting reminder     |  alice@example.com     |  2026-06-05 14:30
  141  |  Weekly report        |  bob@example.com       |  2026-06-04 09:15
```

Single-item commands (`get`, `send`, `reply`) render key-value pairs:
```
UID:          142
Subject:      Meeting reminder
From:         alice@example.com
Date:         2026-06-05 14:30
Body:         Reminder about tomorrow's standup
```

## Sending Email

`emails send` reads JSON from stdin:
```json
{
  "to": ["someone@example.com"],
  "cc": [],
  "subject": "Hello",
  "body": "Plain text body",
  "attachments": ["/path/to/file.pdf"]
}
```

`emails reply <uid>` same interface, auto-populates In-Reply-To, References, Subject: Re:.

## Project Structure

```
agentmail/
├── main.go                 # cobra entry point
├── go.mod / go.sum
├── cmd/
│   ├── root.go
│   ├── accounts.go
│   ├── folders.go
│   ├── emails.go
│   ├── sync.go
│   └── cache.go
├── internal/
│   ├── config/
│   │   └── config.go       # TOML config parsing
│   ├── imap/
│   │   └── client.go       # IMAP operations
│   ├── smtp/
│   │   └── client.go       # SMTP operations
│   ├── cache/
│   │   ├── db.go           # SQLite open/migrate
│   │   ├── accounts.go
│   │   ├── folders.go
│   │   ├── emails.go       # + search
│   │   └── attachments.go
│   ├── oauth2/
│   │   └── oauth2.go       # device code flow
│   └── output/
│       └── output.go       # JSON/text formatter
└── docs/
    └── design.md
```

## Implementation Order

1. Skeleton: `main.go`, `go.mod`, cobra command stubs
2. Config module: TOML parsing, account CRUD
3. SQLite cache layer: schema + migrations + CRUD per table
4. IMAP client: connect, list folders, fetch UIDs, fetch bodies
5. SMTP client: send emails
6. Wire up: emails list/get/search/send/reply/trash
7. OAuth2: device code flow
8. Output formatter (JSON/text)
9. Sync commands, cache status

## Dependencies

- `github.com/emersion/go-imap` — IMAP client
- `github.com/emersion/go-smtp` — SMTP client
- `github.com/mattn/go-sqlite3` — SQLite driver (CGO)
- `github.com/spf13/cobra` — CLI framework
- `github.com/BurntSushi/toml` — TOML parsing
- `golang.org/x/oauth2` — OAuth2
- `github.com/jaytaylor/html2text` — HTML→text conversion
