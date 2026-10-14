package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
	"time"
)

// Pure SQL/Go identity parity only; this is not an admission or RLS proof.
func TestMultiIntentIdentitySQLParity(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHOTO_TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		_, _, _, _, params, _ := multiQualifiedFixture(t, operation)
		who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
		tenant, project := uuid.New(), uuid.New()
		key := uuid.NewString()
		intent := MultiIntent{SchemaVersion: 1, Snapshot: uuid.New(), Params: params}
		expected, err := compileMultiIntentIdentity(who, tenant, project, operation, key, intent)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := canonical.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		var hash string
		err = conn.QueryRow(ctx, `SELECT encode(sha256(convert_to(photo.v27_semantic_policy_json(jsonb_build_object('project_id',$1::uuid,'auth_session_id',$2::uuid,'operation',$3::text,'intent',$4::jsonb)),'UTF8')),'hex')`, project, who.SessionID, operation, string(raw)).Scan(&hash)
		if err != nil || hash != expected.Hash {
			t.Fatal("SQL request hash differs", err, hash, expected.Hash)
		}
		var run uuid.UUID
		err = conn.QueryRow(ctx, `WITH input AS (SELECT public.digest(uuid_send($1::uuid)||convert_to('V29.Multi:'||$2::text||':'||$3::uuid||':'||$4::uuid||':'||$5::text,'UTF8'),'sha1') h), versioned AS (SELECT set_byte(set_byte(h,6,(get_byte(h,6)&15)|80),8,(get_byte(h,8)&63)|128) h FROM input) SELECT encode(substring(h FROM 1 FOR 16),'hex')::uuid FROM versioned`, tenant, operation, project, who.UserID, key).Scan(&run)
		if err != nil || run != expected.Run {
			t.Fatal("SQL run identity differs", err, run, expected.Run)
		}
	}
}
