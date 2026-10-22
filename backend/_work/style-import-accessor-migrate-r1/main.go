// Prepared operator command. Running this applies exactly reviewed migration 2648.
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
	if os.Getenv("CONFIRM_STYLE_IMPORT_ACCESSOR_MIGRATION") != "2647-to-2648" {
		return errors.New("explicit migration confirmation required")
	}
	raw, e := migrations.FS.ReadFile("migrations/02648_style_import_leased_artifact_ref.sql")
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "085aef6f10c64bfc21c92ac94ec75b3e9d5dbb5ded0d5a6cc57bda1a3a6b6ec2" {
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
	if head != 2647 {
		return errors.New("expected exact head 2647")
	}
	goose.SetBaseFS(migrations.FS)
	if e = goose.SetDialect("postgres"); e != nil {
		return e
	}
	if e = goose.UpTo(db, "migrations", 2648); e != nil {
		return migrations.SummarizeError(e)
	}
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2648 {
		return errors.New("unexpected postmigration head")
	}
	if e = migrations.ValidateInstalledSecurity(ctx, db); e != nil {
		return e
	}
	fmt.Println("PINNED_STYLE_IMPORT_ACCESSOR_MIGRATION_2648_APPLIED")
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
