package main

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *store) groupMembers(name string) ([]user, error) {
	var groupID int64
	if err := s.db.QueryRow("SELECT id FROM device_groups WHERE name=?", name).Scan(&groupID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT u.id,u.username,u.display_name,u.is_admin,u.status
FROM users u JOIN device_group_members m ON m.user_id=u.id
WHERE m.group_id=? ORDER BY u.username COLLATE NOCASE`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []user{}
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.ID, &u.Name, &u.DisplayName, &u.IsAdmin, &u.Status); err != nil {
			return nil, err
		}
		items = append(items, u)
	}
	return items, rows.Err()
}

func (s *state) adminGroupMembers(w http.ResponseWriter, r *http.Request) {
	members, err := s.store.groupMembers(r.PathValue("group"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "group not found")
		} else {
			writeAPIError(w, http.StatusInternalServerError, "failed to list group members")
		}
		return
	}
	adminPaginate(w, r, members)
}
