// Prepared operator command. Running this applies exactly reviewed migration 2643.
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
	if os.Getenv("CONFIRM_CONSENT_WITHDRAWAL_MIGRATION") != "2642-to-2643" {
		return errors.New("explicit migration confirmation required")
	}
	raw, e := migrations.FS.ReadFile("migrations/02643_consent_withdrawal_operation_literals.sql")
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "f58ccd68c986a5f2a7c67123c1a2abe9d454f32665d879ee20f7adea61889e77" {
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
	if head != 2642 {
		return errors.New("expected exact head 2642")
	}
	goose.SetBaseFS(migrations.FS)
	if e = goose.SetDialect("postgres"); e != nil {
		return e
	}
	if e = goose.UpTo(db, "migrations", 2643); e != nil {
		return migrations.SummarizeError(e)
	}
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2643 {
		return errors.New("unexpected postmigration head")
	}
	if e = migrations.ValidateInstalledSecurity(ctx, db); e != nil {
		return e
	}
	fmt.Println("PINNED_CONSENT_WITHDRAWAL_MIGRATION_2643_APPLIED")
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
