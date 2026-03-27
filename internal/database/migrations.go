package database

import _ "embed"

//go:embed schema.sql
var ddl string

//go:embed migrations_extra.sql
var ddlExtra string
