package migrations

import "embed"

// FS contains forward-only, reviewed PostgreSQL migrations.
//
//go:embed migrations/*.sql
var FS embed.FS
