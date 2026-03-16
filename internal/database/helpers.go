package database

import "database/sql"

func String(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func Int64(i int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(i), Valid: true}
}

func Int64Value(i int64) sql.NullInt64 {
	return sql.NullInt64{Int64: i, Valid: true}
}

func Float64(f float64) sql.NullFloat64 {
	return sql.NullFloat64{Float64: f, Valid: true}
}
