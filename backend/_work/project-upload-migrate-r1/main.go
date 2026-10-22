// Prepared operator command. Running this applies exactly reviewed migration 2641.
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
	if os.Getenv("CONFIRM_PROJECT_UPLOAD_MIGRATION") != "2640-to-2641" {
		return errors.New("explicit migration confirmation required")
	}
	raw, e := migrations.FS.ReadFile("migrations/02641_user_project_upload_completion.sql")
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "47c64c45dbe46bf419342e5ff0dfd91ca5a9ded71656e979b4edf950bbe19b3a" {
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
	if head != 2640 {
		return errors.New("expected exact head 2640")
	}
	goose.SetBaseFS(migrations.FS)
	if e = goose.SetDialect("postgres"); e != nil {
		return e
	}
	if e = goose.UpTo(db, "migrations", 2641); e != nil {
		return migrations.SummarizeError(e)
	}
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2641 {
		return errors.New("unexpected postmigration head")
	}
	if e = migrations.ValidateInstalledSecurity(ctx, db); e != nil {
		return e
	}
	fmt.Println("PINNED_PROJECT_UPLOAD_MIGRATION_2641_APPLIED")
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
