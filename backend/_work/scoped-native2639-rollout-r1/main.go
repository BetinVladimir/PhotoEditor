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
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type target struct {
	Signature string `json:"signature"`
	Old       string `json:"old_sha256"`
	Next      string `json:"next_sha256"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, e := sql.Open("pgx", os.Getenv("PHOTO_TEST_DATABASE_URL"))
	must(e)
	defer db.Close()
	db.SetMaxOpenConns(1)
	raw, e := os.ReadFile(os.Getenv("EXPECTED_FILE"))
	must(e)
	var ts []target
	must(json.Unmarshal(raw, &ts))
	if len(ts) != 2 {
		panic("target count")
	}
	ts = append(ts, target{"photo.semantic_defect_policy_expectations_match(jsonb)", "c2bfc9613ecf1b7077511d38c18ffced9a5785ad048415dea0800f52393f2d8b", "275490ace08a797fa5a909b0e278bedc8789603ac8d4c3495dd7be9aeb086a78"})
	meta := map[string]string{}
	for _, t := range ts {
		var b, m string
		must(db.QueryRowContext(ctx, "SELECT prosrc,(to_jsonb(p)-'prosrc')::text FROM pg_proc p WHERE oid=$1::regprocedure", t.Signature).Scan(&b, &m))
		if hash(b) != t.Old {
			panic("predecessor body mismatch")
		}
		meta[t.Signature] = m
	}
	others := `SELECT jsonb_agg(to_jsonb(p) ORDER BY p.oid)::text FROM pg_proc p WHERE p.pronamespace='photo'::regnamespace AND p.oid<>$1::regprocedure AND p.oid<>$2::regprocedure AND p.oid<>$3::regprocedure`
	var oldOthers, oldLedger string
	must(db.QueryRowContext(ctx, others, ts[0].Signature, ts[1].Signature, ts[2].Signature).Scan(&oldOthers))
	must(db.QueryRowContext(ctx, "SELECT jsonb_agg(to_jsonb(v) ORDER BY id)::text FROM public.goose_db_version v").Scan(&oldLedger))
	head, e := goose.GetDBVersionContext(ctx, db)
	must(e)
	if head != 2637 {
		panic("requires native2637")
	}
	must(goose.SetDialect("postgres"))
	goose.SetBaseFS(os.DirFS(os.Getenv("MIGRATION_DIRECTORY")))
	must(goose.UpContext(ctx, db, "."))
	head, e = goose.GetDBVersionContext(ctx, db)
	must(e)
	if head != 2639 {
		panic("wrong resulting head")
	}
	for _, t := range ts {
		var b, m string
		must(db.QueryRowContext(ctx, "SELECT prosrc,(to_jsonb(p)-'prosrc')::text FROM pg_proc p WHERE oid=$1::regprocedure", t.Signature).Scan(&b, &m))
		if hash(b) != t.Next || m != meta[t.Signature] {
			panic("target postcondition")
		}
	}
	var nowOthers, prefix string
	must(db.QueryRowContext(ctx, others, ts[0].Signature, ts[1].Signature, ts[2].Signature).Scan(&nowOthers))
	if oldOthers != nowOthers {
		panic("unrelated photo function changed")
	}
	must(db.QueryRowContext(ctx, "SELECT jsonb_agg(to_jsonb(v) ORDER BY id)::text FROM public.goose_db_version v WHERE version_id NOT IN (2638,2639)").Scan(&prefix))
	if prefix != oldLedger {
		panic("original ledger changed")
	}
	must(goose.UpContext(ctx, db, "."))
	var count int
	must(db.QueryRowContext(ctx, "SELECT count(*) FROM public.goose_db_version WHERE version_id IN (2638,2639) AND is_applied").Scan(&count))
	if count != 2 {
		panic("ledger append count")
	}
	must(json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "SCOPED_NATIVE2637_TO2639_GOOSE_PASS", "head": head, "original_ledger_preserved": true, "unrelated_photo_functions_preserved": true, "target_metadata_preserved": true, "noop": true, "down_not_attempted_on_main": true, "broad2640_accepted": false, "portable_installer_accepted": false}))
}
