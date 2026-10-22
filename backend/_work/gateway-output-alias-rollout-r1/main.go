package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"os"
	"time"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type target struct {
	Signature string `json:"signature"`
	Old       string `json:"old_sha256"`
	Next      string `json:"next_sha256"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, e := sql.Open("pgx", os.Getenv("PHOTO_TEST_DATABASE_URL"))
	must(e)
	defer db.Close()
	db.SetMaxOpenConns(1)
	raw, e := os.ReadFile(os.Getenv("EXPECTED_FILE"))
	must(e)
	var targets []target
	must(json.Unmarshal(raw, &targets))
	if len(targets) != 2 {
		panic("target count")
	}
	sigs := []string{targets[0].Signature, targets[1].Signature}
	beforeMeta := map[string]string{}
	for _, t := range targets {
		var body, meta string
		must(db.QueryRowContext(ctx, "SELECT prosrc,(to_jsonb(p)-'prosrc')::text FROM pg_proc p WHERE oid=$1::regprocedure", t.Signature).Scan(&body, &meta))
		if sha(body) != t.Old {
			panic("predecessor mismatch")
		}
		beforeMeta[t.Signature] = meta
	}
	others := `SELECT jsonb_agg(to_jsonb(p) ORDER BY p.oid)::text FROM pg_proc p WHERE p.pronamespace='photo'::regnamespace AND p.oid<>$1::regprocedure AND p.oid<>$2::regprocedure`
	var otherBefore, ledgerBefore string
	must(db.QueryRowContext(ctx, others, sigs[0], sigs[1]).Scan(&otherBefore))
	must(db.QueryRowContext(ctx, "SELECT jsonb_agg(to_jsonb(v) ORDER BY id)::text FROM public.goose_db_version v").Scan(&ledgerBefore))
	must(goose.SetDialect("postgres"))
	goose.SetBaseFS(os.DirFS(os.Getenv("MIGRATION_DIRECTORY")))
	head, e := goose.GetDBVersionContext(ctx, db)
	must(e)
	if head != 2639 {
		panic("expected isolated compatibility2639 baseline")
	}
	must(goose.UpContext(ctx, db, "."))
	head, e = goose.GetDBVersionContext(ctx, db)
	must(e)
	if head != 2640 {
		panic("bad head")
	}
	for _, t := range targets {
		var body, meta string
		must(db.QueryRowContext(ctx, "SELECT prosrc,(to_jsonb(p)-'prosrc')::text FROM pg_proc p WHERE oid=$1::regprocedure", t.Signature).Scan(&body, &meta))
		if sha(body) != t.Next || meta != beforeMeta[t.Signature] {
			panic("target mismatch")
		}
	}
	var otherAfter, prefix string
	must(db.QueryRowContext(ctx, others, sigs[0], sigs[1]).Scan(&otherAfter))
	if otherBefore != otherAfter {
		panic("unrelated function drift")
	}
	must(db.QueryRowContext(ctx, "SELECT jsonb_agg(to_jsonb(v) ORDER BY id)::text FROM public.goose_db_version v WHERE version_id<>2640").Scan(&prefix))
	if prefix != ledgerBefore {
		panic("ledger prefix changed")
	}
	must(goose.UpContext(ctx, db, "."))
	var count int
	must(db.QueryRowContext(ctx, "SELECT count(*) FROM public.goose_db_version WHERE version_id=2640 AND is_applied").Scan(&count))
	if count != 1 {
		panic("ledger append count")
	}
	must(json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "SCOPED_NATIVE2639_TO2640_GOOSE_PASS", "up": true, "noop": true, "down_not_attempted_on_main": true, "original_ledger_unchanged": true, "target_metadata_preserved": true, "other_functions_unchanged": true, "broad_integration2641_accepted": false, "main_database_changed": true}))
}
