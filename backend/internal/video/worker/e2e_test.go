//go:build videoe2e

package worker_test

// Scenario: upload -> probe -> montage -> render -> delivery against a real
// Postgres (migrations 02683-02703) and real ffmpeg. Every step runs under the
// DB role that owns it in production (SET ROLE), so missing grants fail here.
//
//	VIDEO_E2E_DSN='host=... port=... user=... dbname=scratch_v' go test -tags videoe2e ./internal/video/worker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/video/probe"
	"photoeditor/backend/internal/video/worker"
)

const (
	bucket = "video-e2e"
	params = "b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0"
	tps    = 705600000
)

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
	cfg.MaxConns = 4
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func hex64(t *testing.T) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func must(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func lookup(t *testing.T, name string) string {
	if v := os.Getenv(map[string]string{"ffmpeg": "VIDEO_E2E_FFMPEG", "ffprobe": "VIDEO_E2E_FFPROBE"}[name]); v != "" {
		return v
	}
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not installed", name)
	}
	return p
}

func TestUploadProbeMontageRenderDelivery(t *testing.T) {
	dsn := os.Getenv("VIDEO_E2E_DSN")
	if dsn == "" {
		t.Skip("VIDEO_E2E_DSN not set")
	}
	ff, fp := lookup(t, "ffmpeg"), lookup(t, "ffprobe")
	ctx := context.Background()
	admin, api, sched, wpool, portal := rolePool(t, dsn, ""), rolePool(t, dsn, "photo_api"), rolePool(t, dsn, "photo_scheduler"), rolePool(t, dsn, "photo_worker"), rolePool(t, dsn, "photo_portal")
	root := t.TempDir()
	store := worker.LocalStore{Root: filepath.Join(root, "objects")}

	// --- tenant, user, project, quota (setup is not part of the product API)
	var user, tid, pid uuid.UUID
	must(t, admin.QueryRow(ctx, `INSERT INTO photo.users(email_normalized) VALUES($1) RETURNING id`, uuid.NewString()+"@example.invalid").Scan(&user), "user")
	tx, err := admin.Begin(ctx)
	must(t, err, "begin")
	must(t, tx.QueryRow(ctx, `INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('e2e',$1,'{"storage_bytes":1000000000000}','{}') RETURNING id`, user).Scan(&tid), "tenant")
	_, err = tx.Exec(ctx, `INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'owner','active')`, tid, user)
	must(t, err, "membership")
	_, err = tx.Exec(ctx, `INSERT INTO photo.tenant_usage(tenant_id) VALUES($1) ON CONFLICT DO NOTHING`, tid)
	must(t, err, "usage")
	must(t, tx.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Film') RETURNING id`, tid).Scan(&pid), "project")
	must(t, tx.Commit(ctx), "commit")

	// --- two real source files
	gen := func(name string, size string, rate string, ac string, secs string) string {
		p := filepath.Join(root, name)
		out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size="+size+":rate="+rate, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
			"-t", secs, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", ac, p).CombinedOutput()
		must(t, err, "ffmpeg gen: "+string(out))
		return p
	}
	srcs := []string{gen("a.mp4", "640x360", "30", "1", "3"), gen("b.mp4", "1280x720", "25", "2", "2")}

	// --- upload (photo_api) and completion (photo_scheduler)
	const part = 33554432
	var media [2]uuid.UUID
	var shas [2]string
	for i, src := range srcs {
		sha, size, err := worker.HashFile(src)
		must(t, err, "hash")
		shas[i] = sha
		key := fmt.Sprintf("tenants/%s/video/uploads/%s/source", tid, uuid.New())
		_, _, err = store.Put(ctx, bucket, key, src)
		must(t, err, "put source")
		var up uuid.UUID
		must(t, api.QueryRow(ctx, `SELECT photo.begin_video_upload($1,$2,$3,'video',$4,$5,$6,$7,$8,1,$9,$10)`,
			tid, pid, user, filepath.Base(src), size, sha, hex64(t), int64(part), key, size+10_000_000).Scan(&up), "begin upload")
		var ok bool
		must(t, api.QueryRow(ctx, `SELECT photo.bind_video_upload_multipart($1,$2,'mp-1')`, tid, up).Scan(&ok), "bind")
		must(t, api.QueryRow(ctx, `SELECT photo.record_video_upload_part($1,$2,1,'e1',$3)`, tid, up, size).Scan(&ok), "part")
		var tok uuid.UUID
		must(t, api.QueryRow(ctx, `SELECT photo.complete_video_upload($1,$2,'[{"part_no":1,"etag":"e1"}]'::jsonb)`, tid, up).Scan(&tok), "complete")
		must(t, sched.QueryRow(ctx, `SELECT photo.finish_video_upload_completion($1,$2,$3,$4,'v1',$5,'video/mp4')`, tid, up, tok, bucket, sha).Scan(&media[i]), "finish completion")
		// ingest job is created by the scheduler when completion is observed
		var job uuid.UUID
		must(t, sched.QueryRow(ctx, `SELECT photo.create_video_job($1,$2,$3,'ingest',$4,NULL,NULL,$5,$6,'auto',$7,$8,0,1)`,
			tid, pid, user, media[i], uuid.New(), params, int64(3*tps), hex64(t)).Scan(&job), "create ingest job")
	}

	w := &worker.Worker{Pool: wpool, Store: store, FFmpeg: ff, FFprobe: fp, Encoder: "libx264", Bucket: bucket, WorkDir: root}
	for n := 0; n < 2; n++ {
		found, err := w.RunOnce(ctx)
		must(t, err, "run ingest")
		if !found {
			t.Fatal("ingest job not claimed")
		}
	}
	for i, m := range media {
		var state string
		var pj []byte
		must(t, admin.QueryRow(ctx, `SELECT state,probe::text FROM photo.video_media WHERE id=$1`, m).Scan(&state, &pj), "media")
		var f probe.Facts
		must(t, json.Unmarshal(pj, &f), "probe json")
		if state != "ready" || !f.HasVideo || !f.HasAudio || f.Width != []int{640, 1280}[i] {
			t.Fatalf("media %d: state=%s facts=%+v", i, state, f)
		}
	}

	// --- montage: A 0..2 s, then B 0.5..2 s (fill); saved as an immutable revision (photo_api)
	doc := fmt.Sprintf(`{"schema_version":1,"sequence_id":"s","base_revision":0,"timebase":{"ticks_per_second":705600000},
"video":{"fps":{"num":25,"den":1},"width":640,"height":360,"color":{"space":"rec709","range":"limited"},"background":"#000000"},
"audio":{"sample_rate":48000,"channels":2},"tracks":[{"id":"V1","kind":"video","clips":[
{"id":"c1","source":{"kind":"video","media_id":"%s","sha256":"%s"},"at":0,"src_in":0,"src_out":%d,"fit":"fit","effects":[]},
{"id":"c2","source":{"kind":"video","media_id":"%s","sha256":"%s"},"at":%d,"src_in":%d,"src_out":%d,"fit":"fill","audio":{"gain_db":-3,"fade_in":0,"fade_out":0,"mute":false},"effects":[]}]}],
"markers":[],"master":{"effects":[],"audio":{"loudness":{"target_lufs":-14,"true_peak_db":-1}}},"rights":{"music_declared":true}}`,
		media[0], shas[0], 2*tps, media[1], shas[1], 2*tps, tps/2, 2*tps)
	docSum := sha256.Sum256([]byte(doc))
	docSha := hex.EncodeToString(docSum[:])
	var sid uuid.UUID
	must(t, admin.QueryRow(ctx, `INSERT INTO photo.video_sequences(tenant_id,project_id,name,fps_num,fps_den,width,height,created_by) VALUES($1,$2,'Main',25,1,640,360,$3) RETURNING id`, tid, pid, user).Scan(&sid), "sequence")
	docFile := filepath.Join(root, "doc.json")
	must(t, os.WriteFile(docFile, []byte(doc), 0o600), "write doc")
	docKey := fmt.Sprintf("tenants/%s/video/sequences/%s/rev1.json", tid, sid)
	_, _, err = store.Put(ctx, bucket, docKey, docFile)
	must(t, err, "put doc")
	var docArt uuid.UUID
	atx, err := api.Begin(ctx)
	must(t, err, "begin api tx")
	_, err = atx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tid.String()) // as database.TenantTransaction does
	must(t, err, "tenant ctx")
	must(t, atx.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
		VALUES($1,$2,$3,'v1',$4,$5,'application/json','ready',now(),'video_sequence_document') RETURNING id`, tid, bucket, docKey, docSha, len(doc)).Scan(&docArt), "doc artifact")
	must(t, atx.Commit(ctx), "commit doc artifact")
	var rev int64
	srcJSON := fmt.Sprintf(`[{"kind":"video","id":"%s","sha256":"%s"},{"kind":"video","id":"%s","sha256":"%s"}]`, media[0], shas[0], media[1], shas[1])
	must(t, api.QueryRow(ctx, `SELECT photo.save_video_sequence_revision($1,$2,0,$3,$4,'{"clips":2}'::jsonb,$5::jsonb,'manual',$6)`, tid, sid, docArt, docSha, srcJSON, user).Scan(&rev), "save revision")

	// --- render (photo_api creates the job + render row; worker renders)
	var render uuid.UUID
	must(t, api.QueryRow(ctx, `SELECT photo.create_video_render($1,$2,$3,$4,$5,'{"name":"web","encoder":"libx264"}'::jsonb,$6,$7,$8,'auto',$9,$10,50000000,1)`,
		tid, pid, user, sid, rev, hex64(t), uuid.New(), params, int64(35*tps/10), hex64(t)).Scan(&render), "create render")
	found, err := w.RunOnce(ctx)
	must(t, err, "run render")
	if !found {
		t.Fatal("render job not claimed")
	}
	var rstate, enc string
	var outArt uuid.UUID
	var dur int64
	must(t, admin.QueryRow(ctx, `SELECT state,coalesce(encoder,''),output_artifact_id,coalesce(duration_ticks,0) FROM photo.video_renders WHERE id=$1`, render).Scan(&rstate, &enc, &outArt, &dur), "render row")
	if rstate != "ready" || enc != "libx264" {
		var code, detail *string
		_ = admin.QueryRow(ctx, `SELECT error_code,error_detail FROM photo.video_jobs WHERE tenant_id=$1 AND id=(SELECT job_id FROM photo.video_renders WHERE id=$2)`, tid, render).Scan(&code, &detail)
		t.Fatalf("render state=%s encoder=%s job error=%v %v", rstate, enc, code, detail)
	}
	var key string
	must(t, admin.QueryRow(ctx, `SELECT object_key FROM photo.artifacts WHERE id=$1`, outArt).Scan(&key), "out artifact")
	facts, _, err := probe.Run(ctx, fp, filepath.Join(store.Root, bucket, filepath.FromSlash(key)))
	must(t, err, "probe output")
	if facts.Width != 640 || facts.Height != 360 || facts.FPSNum != 25 || !facts.HasAudio || facts.Channels != 2 || dur < 3*tps || dur > 4*tps {
		t.Fatalf("output %+v dur=%d", facts, dur)
	}
	var jstate string
	must(t, admin.QueryRow(ctx, `SELECT state FROM photo.video_jobs WHERE tenant_id=$1 AND id=(SELECT job_id FROM photo.video_renders WHERE id=$2)`, tid, render).Scan(&jstate), "job state")
	if jstate != "succeeded" {
		t.Fatalf("job state %s", jstate)
	}

	// --- delivery (photo_api) and viewer egress (photo_portal)
	var coll uuid.UUID
	must(t, api.QueryRow(ctx, `SELECT photo.create_video_delivery($1,$2,'Wedding film')`, tid, pid).Scan(&coll), "delivery")
	var ord int
	must(t, api.QueryRow(ctx, `SELECT photo.add_video_render_to_delivery($1,$2,$3,'Full film')`, tid, coll, render).Scan(&ord), "add render")
	var ok bool
	must(t, api.QueryRow(ctx, `SELECT photo.publish_video_delivery($1,$2,true)`, tid, coll).Scan(&ok), "publish")
	share := uuid.New()
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	must(t, api.QueryRow(ctx, `SELECT photo.create_video_share($1,$2,$3,$4,'client',true,now()+interval '1 day','{}'::jsonb,$5)`, tid, coll, share, secret, user).Scan(&share), "share")
	must(t, portal.QueryRow(ctx, `SELECT photo.record_video_egress($1,$2,$3,$4)`, tid, share, render, facts_size(t, store, key)).Scan(&ok), "egress")
	if !ok {
		t.Fatal("egress denied for a published, downloadable share")
	}
	must(t, api.QueryRow(ctx, `SELECT photo.revoke_video_share($1,$2)`, tid, share).Scan(&ok), "revoke")
	must(t, portal.QueryRow(ctx, `SELECT photo.record_video_egress($1,$2,$3,1)`, tid, share, render).Scan(&ok), "egress after revoke")
	if ok {
		t.Fatal("egress allowed after revoke")
	}
}

func facts_size(t *testing.T, s worker.LocalStore, key string) int64 {
	_, n, err := worker.HashFile(filepath.Join(s.Root, bucket, filepath.FromSlash(key)))
	must(t, err, "size")
	return n
}
