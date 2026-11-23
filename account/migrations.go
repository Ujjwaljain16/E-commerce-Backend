package account

import "embed"

// Migrations holds the SQL migrations applied at startup.
//
//go:embed migrations/*.sql
var Migrations embed.FS
