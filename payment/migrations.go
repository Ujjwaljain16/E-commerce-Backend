package payment

import "embed"

// Migrations holds the SQL schema, applied at startup with database.Migrate.
//
//go:embed migrations/*.sql
var Migrations embed.FS
