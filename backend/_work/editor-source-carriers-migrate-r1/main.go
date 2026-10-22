// Prepared operator command. Running this applies exactly reviewed migration 2644.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"os"
	migrations "photoeditor/backend/db"
)

func run() error {
	if os.Getenv("CONFIRM_EDITOR_SOURCE_CARRIERS_MIGRATION") != "2643-to-2644" {
		return errors.New("explicit migration confirmation required")
	}
	raw, e := migrations.FS.ReadFile("migrations/02644_editor_source_failure_carriers.sql")
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "db5c353697369753480cd66c3e71450a68d647cbcd97e57eedbe0093c43bf549" {
		return errors.New("migration pin mismatch")
	}
	if e = migrations.ValidateBundle(migrations.FS); e != nil {
		return e
	}
	db, e := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if e != nil {
		return errors.New("database configuration failed")
	}
	defer db.Close()
	ctx := context.Background()
	var head int64
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2643 {
		return errors.New("expected exact head 2643")
	}
	goose.SetBaseFS(migrations.FS)
	if e = goose.SetDialect("postgres"); e != nil {
		return e
	}
	if e = goose.UpTo(db, "migrations", 2644); e != nil {
		return migrations.SummarizeError(e)
	}
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2644 {
		return errors.New("unexpected postmigration head")
	}
	if e = migrations.ValidateInstalledSecurity(ctx, db); e != nil {
		return e
	}
	fmt.Println("PINNED_EDITOR_SOURCE_CARRIERS_MIGRATION_2644_APPLIED")
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
