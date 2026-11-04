//go:build videoe2e

package videoapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	videoapi "photoeditor/backend/internal/video/api"
	"photoeditor/backend/internal/video/worker"
)

// Presigned multipart upload against a real MinIO: VIDEO_E2E_MINIO="host:port,access,secret".
func TestMultipartUploadAgainstMinIO(t *testing.T) {
	dsn, mo := os.Getenv("VIDEO_E2E_DSN"), strings.Split(os.Getenv("VIDEO_E2E_MINIO"), ",")
	if dsn == "" || len(mo) != 3 {
		t.Skip("VIDEO_E2E_DSN / VIDEO_E2E_MINIO not set")
	}
	ctx := context.Background()
	s3, err := worker.NewS3Store(mo[0], "", mo[1], mo[2], false)
	must(t, err, "s3 store")
	const bkt = "video-api-multipart-e2e"
	must(t, s3.EnsureBucket(ctx, bkt), "bucket")
	admin, api, sched := rolePool(t, dsn, ""), rolePool(t, dsn, "photo_api"), rolePool(t, dsn, "photo_scheduler")
	var user, tid, pid uuid.UUID
	must(t, admin.QueryRow(ctx, `INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id`, uuid.NewString()+"@example.invalid").Scan(&user), "user")
	tx, err := admin.Begin(ctx)
	must(t, err, "begin")
	must(t, tx.QueryRow(ctx, `INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('mp e2e',$1,'{"storage_bytes":1000000000000}','{}') RETURNING id`, user).Scan(&tid), "tenant")
	_, err = tx.Exec(ctx, `INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'owner','active')`, tid, user)
	must(t, err, "membership")
	_, err = tx.Exec(ctx, `INSERT INTO photo.tenant_usage(tenant_id) VALUES($1) ON CONFLICT DO NOTHING`, tid)
	must(t, err, "usage")
	must(t, tx.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Film') RETURNING id`, tid).Scan(&pid), "project")
	must(t, tx.Commit(ctx), "commit")
	svc := &videoapi.Service{Pool: api, Scheduler: sched, Store: s3, Multipart: s3, Bucket: bkt, TempDir: t.TempDir(),
		Authz: func(_ context.Context, _ pgx.Tx, who auth.Identity, tt uuid.UUID, _ domain.Role) (domain.Role, error) {
			if tt != tid || who.UserID != user {
				return "", domain.NewError("NOT_FOUND", 404)
			}
			return domain.Owner, nil
		}}
	router := chi.NewRouter()
	videoapi.Routes(router, svc, func(r *http.Request, _ bool) (auth.Identity, uuid.UUID, error) {
		t2, err := uuid.Parse(chi.URLParam(r, "t"))
		return auth.Identity{UserID: user, SessionID: uuid.New()}, t2, err
	})
	c := &client{t: t, h: router, base: "/api/v1/tenants/" + tid.String(), user: user}

	data := make([]byte, 70<<20) // 32 + 32 + 6 MiB
	_, _ = rand.Read(data)
	sum := sha256.Sum256(data)
	declared := hex.EncodeToString(sum[:])

	run := func(name, declaredSHA string) (int, map[string]any) {
		up := c.json("POST", "/projects/"+pid.String()+"/video/uploads", map[string]any{"name": name, "kind": "video", "bytes": len(data), "sha256": declaredSHA}, 201)
		uid := up["upload_id"].(string)
		// parts can only be signed after the multipart upload has been started
		if code, _ := c.do("POST", "/video/uploads/"+uid+"/parts", []byte(`{"part_numbers":[1]}`), nil); code != 409 {
			t.Fatalf("sign before start: %d", code)
		}
		st := c.json("POST", "/video/uploads/"+uid+"/multipart", nil, 200)
		if st["parts_total"].(float64) != 3 {
			t.Fatalf("start: %v", st)
		}
		signed := c.json("POST", "/video/uploads/"+uid+"/parts", map[string]any{"part_numbers": []int{1, 2, 3}}, 200)
		urls := signed["urls"].([]any)
		if len(urls) != 3 {
			t.Fatalf("signed: %v", signed)
		}
		if code, _ := c.do("POST", "/video/uploads/"+uid+"/complete", nil, nil); code != 422 {
			t.Fatalf("complete without parts: %d", code)
		}
		const pb = 32 << 20
		for _, u := range urls {
			m := u.(map[string]any)
			n := int(m["part_no"].(float64))
			lo, hi := (n-1)*pb, min(n*pb, len(data))
			req, _ := http.NewRequest("PUT", m["url"].(string), bytes.NewReader(data[lo:hi]))
			resp, err := http.DefaultClient.Do(req)
			must(t, err, "put part")
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("part %d: %d", n, resp.StatusCode)
			}
		}
		code, b := c.do("POST", "/video/uploads/"+uid+"/complete", nil, nil)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return code, out
	}

	code, out := run("big.mp4", declared)
	if code != 202 || out["state"] != "probing" {
		t.Fatalf("complete: %d %v", code, out)
	}
	var kind, state string
	must(t, admin.QueryRow(ctx, `SELECT kind,state FROM photo.video_media WHERE id=$1`, out["media_id"]).Scan(&kind, &state), "media row")
	if kind != "video" || state != "probing" {
		t.Fatalf("media %s %s", kind, state)
	}
	h, n, err := s3.HashObject(ctx, bkt, mustKey(t, admin, tid, out["media_id"]))
	must(t, err, "hash stored")
	if h != declared || n != int64(len(data)) {
		t.Fatalf("stored object differs: %s %d", h, n)
	}

	// a declaration that does not match the bytes is refused and the object is removed
	code, out = run("lie.mp4", strings.Repeat("0", 64))
	if code != 422 {
		t.Fatalf("mismatch: %d %v", code, out)
	}

	// LUT uploads are accepted up to 20 MiB only
	c.json("POST", "/projects/"+pid.String()+"/video/uploads", map[string]any{"name": "a.cube", "kind": "lut", "bytes": 1000, "sha256": declared}, 201)
	if code, _ := c.do("POST", "/projects/"+pid.String()+"/video/uploads", []byte(`{"name":"b.cube","kind":"lut","bytes":`+"21000000"+`,"sha256":"`+declared+`"}`), map[string]string{"Idempotency-Key": "key-lut-too-big-1"}); code != 422 {
		t.Fatalf("oversize lut: %d", code)
	}
}

func mustKey(t *testing.T, admin interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tid uuid.UUID, media any) string {
	var key string
	must(t, admin.QueryRow(context.Background(), `SELECT a.object_key FROM photo.video_media m JOIN photo.artifacts a ON a.tenant_id=m.tenant_id AND a.id=m.source_artifact_id WHERE m.id=$1`, media).Scan(&key), "object key")
	return key
}
