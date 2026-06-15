# DSH — Architecture Diagram

## System Overview

```mermaid
graph TB
    subgraph "Tomas's machine (laptop or desktop)"
        BROWSER["Browser\n(SSH port-forwarded)"]
    end

    subgraph "Home Server — Docker (network_mode: host)"
        subgraph "dsh container"
            HTTP["Go HTTP Server\n:9090"]
            UI["UI Handler\n/todo /projects /notifications\n/settings/passkeys /admin"]
            API["API Handler\n/api/v1/*"]
            AUTH["Auth Module\n• session middleware\n• Passkey (go-webauthn)\n• OAuth2.1 token issuer"]
            TMPL["html/template\n+ HTMX\n(ASCII style)"]
            DB["SQLite\n/data/dsh.db"]
        end
        VOL[("named volume\n/data")]
        TODO[("todo.txt\nbind mount")]
        PM[("project-management/\nbind mount (ro)")]
    end

    subgraph "Other projects / agents"
        AGENT["Agent or script\n(e.g. SCA, future projects)"]
    end

    BROWSER -->|"SSH tunnel :9090"| HTTP
    HTTP --> UI
    HTTP --> API
    UI --> AUTH
    UI --> TMPL
    API --> AUTH
    AUTH --> DB
    UI --> DB
    UI --> TODO
    UI --> PM
    API --> DB
    DB --- VOL
    AGENT -->|"POST /oauth/token\nclient_id + secret"| AUTH
    AGENT -->|"POST /api/v1/projects\nPOST /api/v1/notifications\nBearer JWT"| API
```

## Request Flow — Passkey Login

```mermaid
sequenceDiagram
    actor T as Tomas
    participant B as Browser
    participant S as Go Server
    participant DB as SQLite

    T->>B: navigate to /
    B->>S: GET /
    S->>B: 302 → /login (no session)
    T->>B: click [Login with Passkey]
    B->>S: GET /auth/passkey/login/begin
    S->>DB: store WebAuthn session
    S->>B: credential assertion options
    B->>B: navigator.credentials.get()
    B->>S: POST /auth/passkey/login/finish
    S->>DB: load credential, verify signature
    S->>DB: create session
    S->>B: {redirect: "/"} + session cookie
    B->>S: GET /
    S->>B: 200 dashboard
```

## Request Flow — First-run Setup

```mermaid
sequenceDiagram
    actor T as Tomas
    participant B as Browser
    participant S as Go Server
    participant DB as SQLite

    T->>B: navigate to / (clean DB)
    B->>S: GET /
    S->>DB: COUNT passkey_credentials = 0
    S->>B: 302 → /setup
    T->>B: enter passkey name, click [Register Passkey]
    B->>S: GET /setup/passkey/begin
    S->>B: credential creation options
    B->>B: navigator.credentials.create()
    B->>S: POST /setup/passkey/finish?name=...
    S->>DB: save credential, create session
    S->>B: {redirect: "/"} + session cookie
    B->>S: GET /
    S->>B: 200 dashboard
```

## Request Flow — API Ingest (OAuth 2.1 client credentials)

```mermaid
sequenceDiagram
    participant A as Agent
    participant S as Go Server
    participant DB as SQLite

    A->>S: POST /oauth/token\nclient_id=X, client_secret=Y\ngrant_type=client_credentials
    S->>DB: lookup client, verify argon2id hash
    DB-->>S: match
    S->>A: {access_token: "JWT...", expires_in: 3600}
    A->>S: POST /api/v1/projects\nAuthorization: Bearer JWT
    S->>DB: validate JWT + check revoked_at
    S->>DB: upsert project row + write audit_log
    S->>A: 200 {ok: true}
```

## Database Schema

```
users
  id            INTEGER PK
  username      TEXT UNIQUE
  password_hash TEXT              -- set at bootstrap, never used for login
  created_at    DATETIME

passkey_credentials
  id            INTEGER PK
  user_id       INTEGER FK→users
  credential_id TEXT UNIQUE
  public_key    BLOB
  sign_count    INTEGER
  flags         INTEGER           -- CredentialFlags byte (BackupEligible etc.)
  name          TEXT              -- user-given name, e.g. "MacBook Touch ID"
  created_at    DATETIME

webauthn_sessions
  id            TEXT PK           -- "login_anon" or "reg_<userID>"
  user_id       INTEGER           -- NULL for login ceremony
  data          TEXT              -- JSON-serialised webauthn.SessionData
  expires_at    DATETIME          -- 5-minute TTL

sessions
  id            TEXT PK           -- random 32-byte hex token
  user_id       INTEGER FK→users
  data          TEXT              -- JSON: {csrf_token}
  created_at    DATETIME
  expires_at    DATETIME          -- 24-hour TTL

oauth2_clients
  client_id         TEXT PK
  client_secret_hash TEXT         -- argon2id
  name              TEXT
  created_at        DATETIME
  revoked_at        DATETIME      -- NULL = active
  last_used_at      DATETIME
  last_used_ip      TEXT

notifications
  id            INTEGER PK
  project_code  TEXT
  message       TEXT
  type          TEXT              -- action_needed/info
  created_at    DATETIME
  dismissed_at  DATETIME          -- NULL = active

audit_log
  id            INTEGER PK
  event         TEXT              -- login_success, passkey_login_failure, api_call, ...
  actor         TEXT              -- username or client_id
  remote_ip     TEXT
  detail        TEXT
  created_at    DATETIME

config
  key           TEXT PK           -- e.g. jwt_secret
  value         TEXT

schema_migrations
  name          TEXT PK           -- migration filename
  applied_at    DATETIME
```

## File-backed Data (bind mounts)

| Mount | Container path | Access | Content |
|---|---|---|---|
| `../../todo.txt` | `/todo.txt` | rw | Todo items — `- [s] text  #Q2 #date` format |
| `../../project-management` | `/pm` | ro | `*/PROJECT.md` files — parsed by pmreader |

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DSH_PORT` | no | `8080` | HTTP listen port |
| `DSH_DB_PATH` | no | `/data/dsh.db` | SQLite file path |
| `DSH_ORIGIN` | yes | — | WebAuthn RP origin, e.g. `http://localhost:9090` |
| `DSH_PM_PATH` | no | — | Path to project-management directory (bind mount) |
| `DSH_TODO_PATH` | no | — | Path to todo.txt file (bind mount) |

## Project Layout

```
projects/DSH-dashboard/
├── Dockerfile
├── docker-compose.yml
├── backup.sh / restore.sh
├── run.sh
├── go.mod / go.sum
├── cmd/dsh/
│   ├── main.go                   -- config, DB init, route registration
│   └── web/
│       ├── templates/            -- Go html/template files (embedded)
│       └── static/               -- style.css + htmx.min.js (embedded)
└── internal/
    ├── auth/                     -- session, passkey/WebAuthn, OAuth2 token issuer, audit
    ├── config/                   -- env var loading
    ├── db/                       -- SQLite connection, migration runner
    │   └── migrations/           -- 001_init.sql … 005_passkey_name.sql
    ├── handler/                  -- HTTP handlers: auth, ui, api, middleware
    ├── model/                    -- shared data types
    ├── pmreader/                 -- PROJECT.md filesystem reader
    └── todoreader/               -- todo.txt file reader/writer
```
