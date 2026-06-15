package auth

import (
	"database/sql"
	"log"
)

// WriteAudit appends an entry to the audit log. Errors are logged but never
// returned — audit failures must not break the operation being audited.
func WriteAudit(db *sql.DB, event, actor, remoteIP, detail string) {
	_, err := db.Exec(
		`INSERT INTO audit_log(event, actor, remote_ip, detail) VALUES(?,?,?,?)`,
		event, actor, remoteIP, detail,
	)
	if err != nil {
		log.Printf("audit log: %v", err)
	}
}
