package util

import (
	"database/sql"
	"time"
)

func ParseSQLiteTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}

	layouts := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, s.String); err == nil {
			return &t
		}
	}
	return nil
}
