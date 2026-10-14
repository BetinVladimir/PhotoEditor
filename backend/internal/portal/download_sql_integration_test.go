package portal

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPortalDownloadLockedAdmissionFailsClosedOnRemovedPermission(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signatures := []string{
		"photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz)",
		"photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)",
	}
	for _, signature := range signatures {
		var definition, owner string
		var secure, portal, worker bool
		err = db.QueryRow(ctx, `SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),
            p.prosecdef,has_function_privilege('photo_portal',p.oid,'EXECUTE'),
            has_function_privilege('photo_worker',p.oid,'EXECUTE')
            FROM pg_proc p WHERE p.oid=$1::regprocedure`, signature).
			Scan(&definition, &owner, &secure, &portal, &worker)
		if err != nil || owner != "photo_fn_owner" || !secure || !portal || worker {
			t.Fatal("portal download locked-admission ACL drift", signature, err)
		}
		if strings.Contains(definition, "l.permissions->'download'<>'true'::jsonb") ||
			!strings.Contains(definition, "l.permissions->'download' IS DISTINCT FROM 'true'::jsonb") ||
			!strings.Contains(definition, "l.policy_version IS DISTINCT FROM s.link_version") {
			t.Fatal("portal download permission may pass on SQL NULL", signature)
		}
	}
}

func TestPortalUploadLockedAdmissionRequiresEveryLimit(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz)"
	var definition, owner string
	var secure, portal, worker bool
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),
  p.prosecdef,has_function_privilege('photo_portal',p.oid,'EXECUTE'),
  has_function_privilege('photo_worker',p.oid,'EXECUTE')
  FROM pg_proc p WHERE p.oid=$1::regprocedure`, signature).
		Scan(&definition, &owner, &secure, &portal, &worker)
	if err != nil || owner != "photo_fn_owner" || !secure || !portal || worker {
		t.Fatal("portal upload locked-admission ACL drift", err)
	}
	for _, required := range []string{
		"jsonb_typeof(l.limits) IS DISTINCT FROM 'object'",
		"jsonb_typeof(l.limits->'max_upload_files') IS DISTINCT FROM 'number'",
		"jsonb_typeof(l.limits->'max_upload_bytes_total') IS DISTINCT FROM 'number'",
		"jsonb_typeof(l.limits->'max_file_bytes') IS DISTINCT FROM 'number'",
		"jsonb_typeof(l.limits->'allowed_formats') IS DISTINCT FROM 'array'",
		"NOT coalesce((l.limits->'allowed_formats' ? p_format),false)",
	} {
		if !strings.Contains(definition, required) {
			t.Fatal("portal upload missing fail-closed limit", required)
		}
	}
}

func TestPortalUploadCompletionRejectsNullPartsBeforeSessionLookup(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var outcome string
	err = db.QueryRow(ctx, `SELECT outcome FROM photo.complete_portal_upload(
  $1,$2,'parts-key',$3,'00000000-0000-4000-8000-000000000001'::uuid,
  1,NULL::jsonb,'00000000-0000-4000-8000-000000000002'::uuid,
  '00000000-0000-4000-8000-000000000003'::uuid,
  '00000000-0000-4000-8000-000000000004'::uuid)`,
		[]byte(strings.Repeat("x", 32)), []byte(strings.Repeat("y", 32)),
		strings.Repeat("a", 64)).Scan(&outcome)
	if err != nil || outcome != "invalid" {
		t.Fatal("NULL multipart inventory reached session admission", err, outcome)
	}
	for _, signature := range []string{
		"photo.attach_portal_multipart(bytea,bytea,uuid,text)",
		"photo.complete_portal_upload(bytea,bytea,text,text,uuid,bigint,jsonb,uuid,uuid,uuid)",
	} {
		var definition string
		if err := db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, signature).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(definition, "l.permissions->'upload' IS DISTINCT FROM 'true'::jsonb") ||
			!strings.Contains(definition, "l.policy_version IS DISTINCT FROM s.link_version") {
			t.Fatal("multipart mutation lacks current locked share policy", signature)
		}
		if strings.Contains(signature, "complete_portal_upload") {
			for _, required := range []string{
				"jsonb_typeof(p_parts) IS DISTINCT FROM 'array'",
				"jsonb_typeof(v) IS DISTINCT FROM 'object'",
				"jsonb_typeof(v->'part_no') IS DISTINCT FROM 'number'",
				"jsonb_typeof(v->'etag') IS DISTINCT FROM 'string'",
				"CASE WHEN jsonb_typeof(v->'part_no')='number' THEN (v->>'part_no')::numeric ELSE NULL END",
			} {
				if !strings.Contains(definition, required) {
					t.Fatal("multipart entry may pass on SQL NULL", required)
				}
			}
		}
	}
}
