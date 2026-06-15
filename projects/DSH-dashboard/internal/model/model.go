package model

import "time"

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type Session struct {
	ID        string
	UserID    int64
	Data      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type BacklogItem struct {
	ID        int64
	Text      string
	Priority  string
	Status    string
	AddedDate string
	UpdatedAt time.Time
}

type Project struct {
	Code         string
	Name         string
	Status       string
	Priority     string
	Lead         string
	CurrentPhase string
	LastUpdated  string
}

// ProjectDetail is the drill-down view of a single project: its parsed
// metadata plus the raw text of PROJECT.md, ASSUMPTIONS.md, and each iteration.
type ProjectDetail struct {
	Project     Project
	Overview    string         // raw PROJECT.md content
	Assumptions string         // raw ASSUMPTIONS.md content ("" if absent)
	Iterations  []IterationDoc // sorted by filename
}

// IterationDoc is one iterations/NNN-*.md file.
type IterationDoc struct {
	Name    string // filename, e.g. "003-implementation.md"
	Title   string // first markdown H1, or the filename if none
	Content string // raw file content
}

type Notification struct {
	ID          int64
	ProjectCode string
	Message     string
	Type        string
	Priority    string
	Link        string
	Comment     string
	CreatedAt   time.Time
	DismissedAt *time.Time
}

type Plan struct {
	ID          int64
	ProjectCode string
	Title       string
	Detail      string
	Status      string
	Comment     string
	CreatedAt   time.Time
	DecidedAt   *time.Time
}

type Thread struct {
	ID           int64
	Subject      string
	RefType      string // "" | notification | plan | project
	RefID        string
	Status       string // open | resolved
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	MessageCount int
}

type ThreadMessage struct {
	ID        int64
	ThreadID  int64
	Author    string
	Body      string
	CreatedAt time.Time
}

type OAuth2Client struct {
	ClientID         string
	ClientSecretHash string
	Name             string
	CreatedAt        time.Time
	RevokedAt        *time.Time
	LastUsedAt       *time.Time
	LastUsedIP       string
}

type AuditEntry struct {
	ID        int64
	Event     string
	Actor     string
	RemoteIP  string
	Detail    string
	CreatedAt time.Time
}

type TOTPCredential struct {
	ID        int64
	UserID    int64
	Secret    string
	Verified  bool
	CreatedAt time.Time
}

type PasskeyCredential struct {
	ID           int64
	UserID       int64
	CredentialID string
	PublicKey    []byte
	SignCount     uint32
	CreatedAt    time.Time
}

type PushSubscription struct {
	ID        int64
	UserID    int64
	Endpoint  string
	KeyP256dh string
	KeyAuth   string
	CreatedAt time.Time
}
