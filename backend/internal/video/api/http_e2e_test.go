//go:build videoe2e

package videoapi_test

// Same scenario as the worker test, but driven through the HTTP API.
//
//	VIDEO_E2E_DSN='host=... port=... user=... dbname=scratch_v' go test -tags videoe2e ./internal/video/api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	videoapi "photoeditor/backend/internal/video/api"
	"photoeditor/backend/internal/video/worker"
)

const bucket = "video-api-e2e"
const tps = 705600000

func rolePool(t *testing.T, dsn, role string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if role != "" {
		cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
			_, err := c.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
			return err
		}
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func must(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

type client struct {
	t    *testing.T
	h    http.Handler
	base string
	user uuid.UUID
	seq  int
}

func (c *client) do(method, path string, body []byte, headers map[string]string) (int, []byte) {
	c.t.Helper()
	r := httptest.NewRequest(method, c.base+path, bytes.NewReader(body))
	if len(body) > 0 && headers["Content-Type"] == "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if _, ok := headers["X-User"]; !ok {
		r.Header.Set("X-User", c.user.String())
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Result().Body)
	return w.Code, b
}

func (c *client) json(method, path string, v any, want int) map[string]any {
	c.t.Helper()
	var body []byte
	if v != nil {
		body, _ = json.Marshal(v)
	}
	c.seq++
	code, b := c.do(method, path, body, map[string]string{"Idempotency-Key": fmt.Sprintf("key-%d-%s", c.seq, uuid.NewString())})
	if code != want {
		c.t.Fatalf("%s %s: %d (want %d) %s", method, path, code, want, b)
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func TestVideoAPIScenario(t *testing.T) {
	dsn := os.Getenv("VIDEO_E2E_DSN")
	if dsn == "" {
		t.Skip("VIDEO_E2E_DSN not set")
	}
	ff, e1 := exec.LookPath("ffmpeg")
	fp, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	admin, api, sched, wpool := rolePool(t, dsn, ""), rolePool(t, dsn, "photo_api"), rolePool(t, dsn, "photo_scheduler"), rolePool(t, dsn, "photo_worker")
	root := t.TempDir()
	store := worker.LocalStore{Root: filepath.Join(root, "objects")}
	var user, other, tid, pid uuid.UUID
	must(t, admin.QueryRow(ctx, `INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id`, uuid.NewString()+"@example.invalid").Scan(&user), "user")
	must(t, admin.QueryRow(ctx, `INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id`, uuid.NewString()+"@example.invalid").Scan(&other), "other")
	tx, err := admin.Begin(ctx)
	must(t, err, "begin")
	must(t, tx.QueryRow(ctx, `INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('api e2e',$1,'{"storage_bytes":1000000000000}','{}') RETURNING id`, user).Scan(&tid), "tenant")
	_, err = tx.Exec(ctx, `INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'owner','active')`, tid, user)
	must(t, err, "membership")
	_, err = tx.Exec(ctx, `INSERT INTO photo.tenant_usage(tenant_id) VALUES($1) ON CONFLICT DO NOTHING`, tid)
	must(t, err, "usage")
	must(t, tx.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Film') RETURNING id`, tid).Scan(&pid), "project")
	must(t, tx.Commit(ctx), "commit")

	svc := &videoapi.Service{Pool: api, Scheduler: sched, Store: store, Bucket: bucket, TempDir: root,
		Authz: func(_ context.Context, _ pgx.Tx, who auth.Identity, t uuid.UUID, _ domain.Role) (domain.Role, error) {
			if t != tid || who.UserID != user {
				return "", domain.NewError("NOT_FOUND", 404)
			}
			return domain.Owner, nil
		}}
	router := chi.NewRouter()
	videoapi.Routes(router, svc, func(r *http.Request, _ bool) (auth.Identity, uuid.UUID, error) {
		u, err := uuid.Parse(r.Header.Get("X-User"))
		if err != nil {
			return auth.Identity{}, uuid.Nil, domain.NewError("UNAUTHENTICATED", 401)
		}
		t, err := uuid.Parse(chi.URLParam(r, "t"))
		return auth.Identity{UserID: u, SessionID: uuid.New()}, t, err
	})
	c := &client{t: t, h: router, base: "/api/v1/tenants/" + tid.String(), user: user}

	// two real sources
	gen := func(name, size, rate, ac string) (string, []byte) {
		p := filepath.Join(root, name)
		out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size="+size+":rate="+rate, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
			"-t", "3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", ac, p).CombinedOutput()
		must(t, err, "gen "+string(out))
		b, _ := os.ReadFile(p)
		return p, b
	}
	var media [2]string
	var shas [2]string
	for i, spec := range [][3]string{{"a.mp4", "640x360", "30"}, {"b.mp4", "1280x720", "25"}} {
		_, data := gen(spec[0], spec[1], spec[2], []string{"1", "2"}[i])
		sum := sha256.Sum256(data)
		shas[i] = hex.EncodeToString(sum[:])
		up := c.json("POST", "/projects/"+pid.String()+"/video/uploads", map[string]any{"name": spec[0], "kind": "video", "bytes": len(data), "sha256": shas[i]}, 201)
		uid := up["upload_id"].(string)
		// a body that does not match the declaration is refused and leaves the upload retryable
		if code, _ := c.do("PUT", "/video/uploads/"+uid+"/content", data[:len(data)-1], map[string]string{"Content-Type": "video/mp4"}); code != 422 {
			t.Fatalf("truncated upload: %d", code)
		}
		// another user cannot upload into it
		if code, _ := c.do("PUT", "/video/uploads/"+uid+"/content", data, map[string]string{"Content-Type": "video/mp4", "X-User": other.String()}); code != 404 {
			t.Fatalf("foreign upload: %d", code)
		}
		code, b := c.do("PUT", "/video/uploads/"+uid+"/content", data, map[string]string{"Content-Type": "video/mp4"})
		if code != 202 {
			t.Fatalf("upload: %d %s", code, b)
		}
		var m struct {
			ID string `json:"media_id"`
		}
		_ = json.Unmarshal(b, &m)
		media[i] = m.ID
	}
	if code, _ := c.do("GET", "/projects/"+pid.String()+"/video/media", nil, map[string]string{"X-User": "bad"}); code != 401 {
		t.Fatalf("unauthenticated: %d", code)
	}

	w := &worker.Worker{Pool: wpool, Store: store, FFmpeg: ff, FFprobe: fp, Encoder: "libx264", Bucket: bucket, WorkDir: root}
	for n := 0; n < 2; n++ {
		found, err := w.RunOnce(ctx)
		must(t, err, "ingest")
		if !found {
			t.Fatal("no ingest job")
		}
	}
	list := c.json("GET", "/projects/"+pid.String()+"/video/media", nil, 200)["items"].([]any)
	if len(list) != 2 {
		t.Fatalf("media list %v", list)
	}
	for _, it := range list {
		m := it.(map[string]any)
		if m["state"] != "ready" || m["has_proxy"] != true {
			t.Fatalf("media %v", m)
		}
	}
	code, proxy := c.do("GET", "/video/media/"+media[0]+"/proxy", nil, nil)
	if code != 200 || len(proxy) < 1000 {
		t.Fatalf("proxy: %d %d bytes", code, len(proxy))
	}

	// sequence + document
	seq := c.json("POST", "/projects/"+pid.String()+"/video/sequences", map[string]any{"name": "Main", "fps_num": 25, "fps_den": 1, "width": 640, "height": 360}, 201)["sequence_id"].(string)
	doc := fmt.Sprintf(`{"schema_version":1,"sequence_id":%q,"base_revision":0,"timebase":{"ticks_per_second":705600000},
"video":{"fps":{"num":25,"den":1},"width":640,"height":360,"color":{"space":"rec709","range":"limited"},"background":"#000000"},
"audio":{"sample_rate":48000,"channels":2},"tracks":[{"id":"V1","kind":"video","clips":[
{"id":"c1","source":{"kind":"video","media_id":%q,"sha256":%q},"at":0,"src_in":0,"src_out":%d,"fit":"fit","effects":[]},
{"id":"c2","source":{"kind":"video","media_id":%q,"sha256":%q},"at":%d,"src_in":%d,"src_out":%d,"fit":"fill","effects":[]}]}],
"markers":[],"master":{"effects":[],"audio":{"loudness":{"target_lufs":-14,"true_peak_db":-1}}},"rights":{"music_declared":false}}`,
		seq, media[0], shas[0], 2*tps, media[1], shas[1], 2*tps, tps/2, 2*tps)
	save := func(expected int, doc string, want int) map[string]any {
		return c.json("PUT", "/video/sequences/"+seq+"/document", map[string]any{"expected_revision": expected, "document": json.RawMessage(doc)}, want)
	}
	// a clip pointing at an unknown source is rejected by the validator
	bad := fmt.Sprintf(`{"schema_version":1,"sequence_id":%q,"tracks":[]}`, seq)
	if r := save(0, bad, 422); r["code"] != "VIDEO_DOCUMENT_INVALID" {
		t.Fatalf("invalid doc: %v", r)
	}
	if r := save(0, doc, 200); r["revision"].(float64) != 1 {
		t.Fatalf("save: %v", r)
	}
	// the same save again is an idempotent replay; a different document at the stale revision conflicts
	if r := save(0, doc, 200); r["revision"].(float64) != 1 {
		t.Fatalf("replay: %v", r)
	}
	edited := strings.Replace(doc, `"markers":[]`, `"markers":[{"id":"m1","at":0,"kind":"user","label":"x"}]`, 1)
	if r := save(0, edited, 409); r["head_revision"].(float64) != 1 {
		t.Fatalf("conflict: %v", r)
	}
	got := c.json("GET", "/video/sequences/"+seq, nil, 200)
	if got["head_revision"].(float64) != 1 || got["doc"] == nil {
		t.Fatalf("sequence: %v", got)
	}

	// render
	rid := c.json("POST", "/video/sequences/"+seq+"/renders", map[string]any{"revision": 1, "preset": map[string]any{"name": "web", "encoder": "libx264"}}, 202)["render_id"].(string)
	if r := c.json("GET", "/video/renders/"+rid, nil, 200); r["state"] != "queued" {
		t.Fatalf("render before worker: %v", r)
	}
	if code, _ := c.do("GET", "/video/renders/"+rid+"/content", nil, nil); code != 409 {
		t.Fatalf("content before ready: %d", code)
	}
	found, err := w.RunOnce(ctx)
	must(t, err, "render")
	if !found {
		t.Fatal("no render job")
	}
	r := c.json("GET", "/video/renders/"+rid, nil, 200)
	if r["state"] != "ready" || r["job_state"] != "succeeded" || r["width"].(float64) != 640 {
		t.Fatalf("render: %v", r)
	}
	code, out := c.do("GET", "/video/renders/"+rid+"/content", nil, map[string]string{"Range": "bytes=0-99"})
	if code != 206 || len(out) != 100 {
		t.Fatalf("range read: %d %d", code, len(out))
	}

	// delivery
	cid := c.json("POST", "/projects/"+pid.String()+"/video/deliveries", map[string]any{"name": "Wedding film"}, 201)["delivery_id"].(string)
	c.json("POST", "/video/deliveries/"+cid+"/renders", map[string]any{"render_id": rid, "title": "Full film"}, 200)
	c.json("POST", "/video/deliveries/"+cid+"/publish", map[string]any{"published": true}, 200)
}
