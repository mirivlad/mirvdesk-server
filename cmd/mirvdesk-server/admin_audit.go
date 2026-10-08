package main

import (
	"log"
	"net/http"
	"time"
)

type adminAuditEntry struct {
	ID         int64  `json:"id"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	CreatedAt  int64  `json:"created_at"`
}

func (s *store) writeAdminAudit(actorID int64, action, targetType, targetID string) error {
	_, err := s.db.Exec(`INSERT INTO admin_audit(actor_user_id,action,target_type,target_id,created_at)
VALUES(?,?,?,?,?)`, actorID, action, targetType, targetID, time.Now().Unix())
	return err
}

func (s *store) listAdminAudit() ([]adminAuditEntry, error) {
	rows, err := s.db.Query(`SELECT a.id,COALESCE(u.username,'<deleted>'),
a.action,a.target_type,a.target_id,a.created_at
FROM admin_audit a LEFT JOIN users u ON u.id=a.actor_user_id
ORDER BY a.id DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []adminAuditEntry{}
	for rows.Next() {
		var a adminAuditEntry
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.TargetType, &a.TargetID, &a.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *state) recordAdminAudit(r *http.Request, action, targetType, targetID string) {
	user, _, err := s.authenticatedUser(r)
	if err != nil || !user.IsAdmin {
		return
	}
	if err := s.store.writeAdminAudit(user.ID, action, targetType, targetID); err != nil {
		log.Printf("admin audit write failed: %v", err)
	}
}

func (s *state) adminListAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.listAdminAudit()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list audit events")
		return
	}
	adminPaginate(w, r, items)
}

// Prevent accidental logging of user credentials. Only coarse action names,
// target identifiers and the authenticated administrator are retained.
