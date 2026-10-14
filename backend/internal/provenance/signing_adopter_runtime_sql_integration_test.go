package provenance

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationSigningAdopterRuntimeAdmission(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through V31 adopter runtime required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, signature := range []string{
		"photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text)",
		"photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)",
	} {
		var definition string
		if err := tx.QueryRow(ctx, "SELECT pg_get_functiondef($1::regprocedure)", signature).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(definition, "v31_signing_adopter_runtime_ready") {
			t.Fatal("signing admission bypasses adopter", signature)
		}
	}
	instance, policy := uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	params := `{"schema_version":1,"signing_policy_id":"` + policy.String() + `","policy":{"sha256":"` + sha + `"}}`
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	var diagnosticDefined, privateClone bool
	if err := tx.QueryRow(ctx, `SELECT
  has_function_privilege('photo_api','photo.v31_signing_adopter_unavailable(uuid,uuid,uuid,text)','EXECUTE'),
  NOT has_function_privilege('photo_api','photo.v31_signing_admission_without_runtime(uuid,uuid,uuid,uuid,jsonb,text,text)','EXECUTE')`).Scan(&diagnosticDefined, &privateClone); err != nil || !diagnosticDefined || !privateClone {
		t.Fatal("signing diagnostic ACL incorrect", err)
	}
	var unavailable bool
	if err := tx.QueryRow(ctx, `SELECT photo.v31_signing_adopter_unavailable($1,$2,$3,$4)`,
		uuid.New(), uuid.New(), uuid.New(), `{}`).Scan(&unavailable); err != nil || unavailable {
		t.Fatal("unproven signing request classified as outage", err)
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.v31_signing_adopter_runtime_ready($1,$2)", "photo", params).Scan(&ready); err != nil || ready {
		t.Fatal("missing adopter lease accepted", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	var accepted bool
	if err := tx.QueryRow(ctx, "SELECT photo.announce_v31_signing_adopter_runtime($1,$2,$3::jsonb)",
		instance, "photo", `[{"id":"`+policy.String()+`","sha":"`+sha+`"}]`).Scan(&accepted); err != nil || !accepted {
		t.Fatal("scheduler lease rejected", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, "SELECT photo.v31_signing_adopter_runtime_ready($1,$2)", "photo", params).Scan(&ready); err != nil || !ready {
		t.Fatal("exact installed policy unavailable", err)
	}
	for _, other := range []string{"other", strings.Replace(params, sha, strings.Repeat("b", 64), 1)} {
		bucket, body := "photo", params
		if other == "other" {
			bucket = other
		} else {
			body = other
		}
		if err := tx.QueryRow(ctx, "SELECT photo.v31_signing_adopter_runtime_ready($1,$2)", bucket, body).Scan(&ready); err != nil || ready {
			t.Fatal("foreign signing runtime accepted", err)
		}
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT photo.withdraw_v31_signing_adopter_runtime($1)", instance); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, "SELECT photo.v31_signing_adopter_runtime_ready($1,$2)", "photo", params).Scan(&ready); err != nil || ready {
		t.Fatal("withdrawn signer adopter still ready", err)
	}
}
