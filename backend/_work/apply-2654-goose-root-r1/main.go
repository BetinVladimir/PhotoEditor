// Owned-clone rehearsal only. This binary refuses MAIN database names.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"io/fs"
	"os"
	migrations "photoeditor/backend/db"
	"strings"
	"testing/fstest"
	"time"
)

func digest(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func pinned(path, sha string) ([]byte, error) {
	b, e := os.ReadFile(path)
	if e != nil || digest(b) != sha {
		return nil, errors.New("reviewed source drift")
	}
	return b, nil
}
func run() error {
	if os.Getenv("CONFIRM_APPLY_2654_CLONE") != "owned-clone-2653-to-2654" {
		return errors.New("explicit owned clone confirmation required")
	}
	dsn := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(dsn) == "" {
		return errors.New("explicit database required")
	}
	cfg, e := pgx.ParseConfig(dsn)
	if e != nil {
		return errors.New("invalid database configuration")
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 57432 || cfg.User != "rtx_snapshot_admin" || !strings.HasPrefix(cfg.Database, "photoeditor_apply2654_") || len(cfg.Database) <= len("photoeditor_apply2654_") {
		return errors.New("owned clone only; MAIN refused")
	}
	raw, e := pinned("/Users/freelancer/Documents/PhotoEditor/work/apply-locator-fix-r1/02654_apply_lease_artifact_locators.sql", "5e8d6729e773fc2b8e39409912ceb227016c366343aa833c12b2e6a66b116159")
	if e != nil {
		return e
	}
	before, e := pinned("/Users/freelancer/Documents/PhotoEditor/work/apply-2654-goose-root-r1/guards-before.sql", "f9fd27887ba375c8e27b9837fa3f55d1d8ba1181708a7d512d9757be3519f002")
	if e != nil {
		return e
	}
	after, e := pinned("/Users/freelancer/Documents/PhotoEditor/work/apply-2654-goose-root-r1/guards-after.sql", "71d40e090716dae2286a90fa478ac5009e5fe7b90c7ebc86c296f6bc995f11ed")
	if e != nil {
		return e
	}
	up := "-- +goose Up\n"
	down := "-- +goose Down"
	if strings.Count(string(raw), up) != 1 || strings.Count(string(raw), down) != 1 || strings.Contains(string(raw), "-- +goose NO TRANSACTION") {
		return errors.New("transaction anchors invalid")
	}
	derived := strings.Replace(string(raw), up, up+"-- +goose StatementBegin\n"+string(before)+"-- +goose StatementEnd\n", 1)
	derived = strings.Replace(derived, down, "-- +goose StatementBegin\n"+string(after)+"-- +goose StatementEnd\n"+down, 1)
	source := fstest.MapFS{}
	e = fs.WalkDir(migrations.FS, "migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(migrations.FS, path)
		if err != nil {
			return err
		}
		source[path] = &fstest.MapFile{Data: b}
		return nil
	})
	if e != nil {
		return e
	}
	const target = "migrations/02654_apply_lease_artifact_locators.sql"
	if _, exists := source[target]; exists {
		return errors.New("candidate unexpectedly promoted; rebuild reviewed operator")
	}
	source[target] = &fstest.MapFile{Data: []byte(derived)}
	if e = migrations.ValidateBundle(source); e != nil {
		return e
	}
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		return errors.New("database open failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var head int64
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2653 {
		return errors.New("head2653 required")
	}
	if e = migrations.ValidateInstalledSecurity(ctx, db); e != nil {
		return e
	}
	goose.SetBaseFS(source)
	if e = goose.SetDialect("postgres"); e != nil {
		return e
	}
	if e = goose.UpToContext(ctx, db, "migrations", 2654); e != nil {
		return e
	}
	if e = migrations.ValidateInstalledSecurity(ctx, db); e != nil {
		return e
	}
	if e = db.QueryRowContext(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&head); e != nil {
		return e
	}
	if head != 2654 {
		return errors.New("posthead2654 required")
	}
	fmt.Println("OWNED_CLONE_GOOSE_2654_COMMITTED_FULL_CATALOG_GUARD_PASS")
	return nil
}
func main() {
	if e := run(); e != nil {
		var p *pgconn.PgError
		if errors.As(e, &p) {
			fmt.Fprintf(os.Stderr, "SQLSTATE=%s\n", p.Code)
		}
		fmt.Fprintf(os.Stderr, "FAILED_CLASS=%T\n", e)
		os.Exit(1)
	}
}
