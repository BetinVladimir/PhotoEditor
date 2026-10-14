package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestIntegrationOpenEyesBeforeMediaRequiresExactDonorVersion(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through 01957 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	for _, signature := range []string{
		"photo.v26_review_media_source_ready(uuid)",
		"photo.enqueue_v26_review_before_media(uuid,uuid)",
	} {
		var owner, definition string
		var secured, public bool
		err = conn.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
 WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure($1)`, signature).
			Scan(&owner, &secured, &definition, &public)
		if err != nil {
			t.Fatal(err)
		}
		if owner != "photo_fn_owner" || !secured || public ||
			!strings.Contains(definition, "portrait.open_eyes") ||
			!strings.Contains(definition, "v28_donor_descriptor_jcs") ||
			!strings.Contains(definition, "asset_version_id") {
			t.Fatal("donor before-media authority drift", signature)
		}
	}
	var ready bool
	if err = conn.QueryRow(ctx, `SELECT photo.v26_review_media_source_ready($1)`, uuid.New()).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("unknown donor review media job marked ready")
	}
	var recovered bool
	if err = conn.QueryRow(ctx, `SELECT photo.enqueue_next_v28_open_eyes_before_media()`).Scan(&recovered); err != nil {
		t.Fatal(err)
	}
	if recovered {
		t.Fatal("unexpected pending donor review recovered")
	}
}
