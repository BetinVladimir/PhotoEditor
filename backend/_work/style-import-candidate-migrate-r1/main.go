// Prepared operator command. Running this applies exactly reviewed migration 2649.
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
	if os.Getenv("CONFIRM_STYLE_IMPORT_CANDIDATE_MIGRATION") != "2648-to-2649" {
		return errors.New("explicit migration confirmation required")
	}
	raw, e := migrations.FS.ReadFile("migrations/02649_style_import_candidate_discovery.sql")
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "015470aed4fc7524cfa4b91b94570da30c5e9b4891937b4d0dfd8d0ed2994e4f" {
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
	if head != 2648 {
		return errors.New("expected exact head 2648")
	}
	goose.SetBaseFS(migrations.FS)
	if e = goose.SetDialect("postgres"); e != nil {
		return e
	}
	if e = goose.UpTo(db, "migrations", 2649); e != nil {
		return migrations.SummarizeError(e)
	}
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2649 {
		return errors.New("unexpected postmigration head")
	}
	if e = migrations.ValidateInstalledSecurity(ctx, db); e != nil {
		return e
	}
	fmt.Println("PINNED_STYLE_IMPORT_CANDIDATE_MIGRATION_2649_APPLIED")
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
