package migrations

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const securityValidationTimeout = 10 * time.Second

// maxMigrationSourceBytes is a deployment input budget, not a SQL permission.
// Exact catalog-CAS migrations currently exceed 26 MiB. 64 MiB admits more than
// twice that source size while keeping each read and its text copy bounded.
// Larger captures require an explicitly reviewed transport redesign; never
// truncate a guard or silently lift this ceiling from caller configuration.
const maxMigrationSourceBytes int64 = 64 << 20

func readMigrationSource(source fs.FS, path string, limit int64) ([]byte, error) {
	file, err := source.Open(path)
	if err != nil {
		return nil, errors.New("migration source unavailable or oversized")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		file.Close()
		return nil, errors.New("migration source unavailable or oversized")
	}
	// Stat is only an early rejection. A growing or dishonest filesystem cannot
	// force an unbounded ReadFile allocation or hide a byte past the limit.
	raw, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return nil, errors.New("migration source unavailable or oversized")
	}
	return raw, nil
}

var migrationName = regexp.MustCompile(`^([0-9]{5})_[a-z0-9_]+\.sql$`)

// ValidateBundle runs before Goose changes any database. Gaps in historical
// version numbers and historically absent/comment-only Down sections are allowed.
// This validates bundle structure, not SQL semantics. Production exposes only Up;
// textual Down comments/exception substrings are not proof of safe rollback.
func ValidateBundle(source fs.FS) error {
	entries, err := fs.ReadDir(source, "migrations")
	if err != nil {
		return errors.New("migration bundle unavailable")
	}
	versions := make(map[int64]bool)
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			return errors.New("unexpected migration directory")
		}
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			return errors.New("invalid migration filename")
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version <= 0 || versions[version] {
			return errors.New("duplicate or invalid migration version")
		}
		versions[version] = true
		raw, err := readMigrationSource(source, "migrations/"+entry.Name(), maxMigrationSourceBytes)
		if err != nil {
			return errors.New("migration source unavailable or oversized")
		}
		text := string(raw)
		if strings.Count(text, "-- +goose Up") != 1 || strings.Count(text, "-- +goose Down") > 1 {
			return errors.New("migration sections invalid")
		}
		up, _, _ := strings.Cut(text, "-- +goose Down")
		if !strings.Contains(up, "-- +goose Up") {
			return errors.New("migration Up section must precede Down")
		}
		// Legacy files may omit Down entirely or have only an irreversible comment.
		// Do not rewrite already applied history or infer SQL behavior from casing.
		count++
	}
	if count == 0 {
		return errors.New("empty migration bundle")
	}
	return nil
}

// ValidateInstalledSecurity checks final catalog state after fresh/upgrade, not
// obsolete declarations in earlier files. It does not grant or repair access.
func ValidateInstalledSecurity(parent context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("migration security database unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, securityValidationTimeout)
	defer cancel()
	var ready bool
	err := database.QueryRowContext(ctx, installedSecuritySQL).Scan(&ready)
	if err != nil || !ready {
		return errors.New("installed database security invariants unavailable")
	}
	return ctx.Err()
}

const installedSecuritySQL = `SELECT
 EXISTS(SELECT FROM pg_roles WHERE rolname='photo_fn_owner' AND NOT rolcanlogin AND NOT rolsuper AND NOT rolbypassrls)
 AND EXISTS(SELECT FROM pg_namespace WHERE nspname='photo')
 AND NOT EXISTS(
  SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
  WHERE n.nspname='photo' AND p.prosecdef AND (
   pg_get_userbyid(p.proowner)<>'photo_fn_owner'
   OR NOT coalesce(p.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[]
                  OR p.proconfig @> ARRAY['search_path=pg_catalog,photo']::text[],false)
   OR EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
             WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
  )
 )`
