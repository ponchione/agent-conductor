package database

import "database/sql"

func String(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func Int64(i int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(i), Valid: true}
}
