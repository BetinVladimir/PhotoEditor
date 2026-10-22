package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/styleupload"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type styleHostileActor struct{ Tenant, Project, Actor, Session uuid.UUID }
type styleHostileFixture struct {
	A, B         styleHostileActor
	Objects      objectstore.Config
	Path, SHA256 string
}

// This owner must issue two genuine fresh private tenants/projects/editor sessions.
// No direct ready/adopted/qualified row construction occurs in this suite.
func styleHostileInputs(t *testing.T) (context.Context, styleHostileFixture, styleupload.Service, *objectstore.Store, *pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	raw, dsn := os.Getenv("PHOTO_EDITOR_STYLE_HOSTILES"), os.Getenv("PHOTO_EDITOR_STYLE_HOSTILE_OWNER_DSN")
	maintenanceRaw := os.Getenv("PHOTO_EDITOR_STYLE_MAINTENANCE_CREDENTIAL")
	if raw == "" && dsn == "" && maintenanceRaw == "" {
		t.Skip("owned genuine two-tenant Style lifecycle fixture not configured")
	}
	if raw == "" || dsn == "" || maintenanceRaw == "" {
		t.Fatal("partial hostile fixture")
	}
	var f styleHostileFixture
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.A.Tenant == uuid.Nil || f.B.Tenant == uuid.Nil || f.A.Tenant == f.B.Tenant || f.A.Project == f.B.Project || !filepath.IsAbs(f.Path) {
		t.Fatal("closed real two-tenant fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Second)
	t.Cleanup(cancel)
	api := genuineDedicatedPool(t, ctx, "api")
	maintenance := styleMaintenancePool(t, ctx, maintenanceRaw)
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal("private readonly audit DSN required")
	}
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	audit, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal("audit pool")
	}
	t.Cleanup(audit.Close)
	var pending int
	if e := audit.QueryRow(ctx, `SELECT count(*) FROM photo.style_package_uploads WHERE state NOT IN('ready','aborted')`).Scan(&pending); e != nil || pending != 0 {
		t.Fatal("fresh isolated per-root database with no pending package rows required")
	}

	objects, e := objectstore.New(f.Objects)
	if e != nil {
		t.Fatal("owned physical object configuration")
	}
	return ctx, f, styleupload.Service{DB: api, Objects: objects, Bucket: f.Objects.Bucket}, objects, maintenance, audit
}
func styleWho(a styleHostileActor) auth.Identity {
	return auth.Identity{UserID: a.Actor, SessionID: a.Session}
}
func styleBegin(t *testing.T, ctx context.Context, f styleHostileFixture, s styleupload.Service, a styleHostileActor) styleupload.Session {
	t.Helper()
	r, e := os.Open(f.Path)
	if e != nil {
		t.Fatal("real source required")
	}
	defer r.Close()
	i, e := r.Stat()
	if e != nil || !i.Mode().IsRegular() || i.Size() < 2 || i.Size() > styleupload.MaxBytes {
		t.Fatal("bounded regular original package")
	}
	hash := sha256.New()
	n, e := io.Copy(hash, io.LimitReader(r, styleupload.MaxBytes+1))
	if e != nil || n != i.Size() || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		t.Fatal("original physical source SHA")
	}
	session, e := s.Begin(ctx, styleWho(a), a.Tenant, a.Project, uuid.NewString(), styleupload.BeginRequest{Filename: filepath.Base(f.Path), Bytes: n, SHA256: f.SHA256})
	if e != nil {
		t.Fatal("real API Begin", e)
	}
	return session
}
func stylePut(t *testing.T, ctx context.Context, f styleHostileFixture, s styleupload.Service, x styleupload.Session) []objectstore.UploadedPart {
	t.Helper()
	numbers := make([]int, x.Parts)
	for i := range numbers {
		numbers[i] = i + 1
	}
	signed, e := s.Sign(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, x.ID, numbers)
	if e != nil || len(signed) != x.Parts {
		t.Fatal("actual signing", e)
	}
	endpoint, e := url.Parse(f.Objects.PublicEndpoint)
	if e != nil || endpoint.Host == "" {
		t.Fatal("owned object endpoint")
	}
	file, e := os.Open(f.Path)
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	parts := make([]objectstore.UploadedPart, x.Parts)
	seen := map[int]bool{}
	client := http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, p := range signed {
		u, e := url.Parse(p.URL)
		if e != nil || u.User != nil || u.Fragment != "" || u.Host != endpoint.Host || u.Scheme != endpoint.Scheme || p.PartNumber < 1 || p.PartNumber > x.Parts || seen[p.PartNumber] {
			t.Fatal("signed part inventory escaped owned endpoint")
		}
		seen[p.PartNumber] = true
		offset := int64(p.PartNumber-1) * x.PartBytes
		n := min(x.PartBytes, x.Bytes-offset)
		req, e := http.NewRequestWithContext(ctx, "PUT", p.URL, io.NewSectionReader(file, offset, n))
		if e != nil {
			t.Fatal(e)
		}
		req.ContentLength = n
		res, e := client.Do(req)
		if e != nil {
			t.Fatal("real PUT")
		}
		_, re := io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		ce := res.Body.Close()
		etag := res.Header.Get("ETag")
		if re != nil || ce != nil || res.StatusCode < 200 || res.StatusCode >= 300 || etag == "" {
			t.Fatal("PUT not confirmed")
		}
		parts[p.PartNumber-1] = objectstore.UploadedPart{PartNumber: p.PartNumber, ETag: etag}
	}
	return parts
}
func TestIntegrationStylePackageCompleteAbortRace(t *testing.T) {
	ctx, f, s, _, _, audit := styleHostileInputs(t)
	x := styleBegin(t, ctx, f, s, f.A)
	parts := stylePut(t, ctx, f, s, x)
	request := styleupload.CompleteRequest{Parts: parts, ExpectedSize: x.Bytes}
	key := uuid.NewString()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var completed styleupload.Session
	var completeErr, abortErr error
	go func() {
		defer wg.Done()
		<-start
		completed, completeErr = s.Complete(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, x.ID, key, request)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, abortErr = s.Abort(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, x.ID)
	}()
	close(start)
	wg.Wait()
	actual, e := s.Get(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, x.ID)
	if e != nil {
		t.Fatal("authoritative race readback", e)
	}
	if actual.State == "ready" {
		if completeErr != nil || completed.Artifact == nil || actual.Artifact == nil || *completed.Artifact != *actual.Artifact || abortErr == nil {
			t.Fatal("ready winner must be actual completed and immutable")
		}
		replay, e := s.Complete(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, x.ID, key, request)
		if e != nil || replay.Artifact == nil || *replay.Artifact != *actual.Artifact {
			t.Fatal("exact completion replay")
		}
		if _, e := s.Complete(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, x.ID, uuid.NewString(), request); e == nil {
			t.Fatal("different completion key accepted")
		}
	} else if actual.State == "aborting" {
		if abortErr != nil || completeErr == nil {
			t.Fatal("abort winner must prevent physical ready publication")
		}
	} else {
		t.Fatal("race left unexpected transient state", actual.State)
	}
	var ready int
	if e := audit.QueryRow(ctx, `SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1 AND id=(SELECT artifact_id FROM photo.style_package_uploads WHERE tenant_id=$1 AND id=$2) AND state='ready'`, f.A.Tenant, x.ID).Scan(&ready); e != nil {
		t.Fatal("readonly artifact audit")
	}
	if (actual.State == "ready") != (ready == 1) {
		t.Fatal("ledger/actual artifact disagreement")
	}
	// Signed URLs have a real horizon: external owned tenant teardown must wait it.
}
func TestIntegrationStylePackageCleanupTenantIsolation(t *testing.T) {
	ctx, f, s, objects, maintenance, audit := styleHostileInputs(t)
	var reservedBefore int64
	if e := audit.QueryRow(ctx, `SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1`, f.A.Tenant).Scan(&reservedBefore); e != nil {
		t.Fatal("readonly quota baseline")
	}
	x := styleBegin(t, ctx, f, s, f.A)
	other := styleBegin(t, ctx, f, s, f.B)
	var reservedAfter int64
	if e := audit.QueryRow(ctx, `SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1`, f.A.Tenant).Scan(&reservedAfter); e != nil || reservedAfter != reservedBefore+x.Bytes {
		t.Fatal("actual Begin reservation accounting")
	}
	if _, e := s.Get(ctx, styleWho(f.B), f.A.Tenant, f.A.Project, x.ID); e == nil {
		t.Fatal("cross-tenant actor read allowed")
	}
	if _, e := s.Abort(ctx, styleWho(f.B), f.A.Tenant, f.A.Project, x.ID); e == nil {
		t.Fatal("cross-tenant actor abort allowed")
	}
	if _, e := s.Abort(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, x.ID); e != nil {
		t.Fatal("actual abort", e)
	}
	conn, e := maintenance.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	var guc string
	if e := conn.QueryRow(ctx, `SELECT coalesce(current_setting('app.tenant_id',true),'')`).Scan(&guc); e != nil || guc != "" {
		t.Fatal("fresh dedicated maintenance connection must have no tenant GUC")
	}
	var tenant, id, token uuid.UUID
	var started bool
	var multipart *string
	if e := conn.QueryRow(ctx, `SELECT tenant_id,upload_id,cleanup_token,create_started,multipart_id FROM photo.claim_style_package_cleanup()`).Scan(&tenant, &id, &token, &started, &multipart); e != nil || tenant != f.A.Tenant || id != x.ID || !started || multipart == nil {
		t.Fatal("claim actual API-created unsignaled upload without GUC", e)
	}
	var ok bool
	if e := conn.QueryRow(ctx, `SELECT photo.finish_style_package_cleanup($1,$2,$3)`, tenant, id, uuid.New()).Scan(&ok); e != nil || ok {
		t.Fatal("wrong cleanup fence accepted")
	}
	if _, e := conn.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, f.B.Tenant.String()); e != nil {
		t.Fatal(e)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT set_config('app.tenant_id','',false)`)
	if e := stylePhysicalPurge(ctx, objects, tenant, id); e != nil {
		t.Fatal("physical exact-key cleanup", e)
	}
	if e := conn.QueryRow(ctx, `SELECT photo.finish_style_package_cleanup($1,$2,$3)`, tenant, id, token).Scan(&ok); e != nil || !ok {
		t.Fatal("wrong tenant GUC hid owner-fenced cleanup", e)
	}
	if e := conn.QueryRow(ctx, `SELECT photo.finish_style_package_cleanup($1,$2,$3)`, tenant, id, token).Scan(&ok); e != nil || !ok {
		t.Fatal("cleanup exact replay")
	}
	var state, reservation string
	if e := audit.QueryRow(ctx, `SELECT u.state,q.state FROM photo.style_package_uploads u JOIN photo.quota_reservations q ON q.tenant_id=u.tenant_id AND q.id=u.reservation_id WHERE u.tenant_id=$1 AND u.id=$2`, tenant, id).Scan(&state, &reservation); e != nil || state != "aborted" || reservation != "released" {
		t.Fatal("physical cleanup did not settle quota", e)
	}
	if e := audit.QueryRow(ctx, `SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1`, f.A.Tenant).Scan(&reservedAfter); e != nil || reservedAfter != reservedBefore {
		t.Fatal("quota release changed unrelated accounting")
	}
	if actual, e := s.Get(ctx, styleWho(f.B), f.B.Tenant, f.B.Project, other.ID); e != nil || actual.State != "uploading" {
		t.Fatal("wrong-GUC cleanup altered other tenant")
	}
	if _, e := s.Abort(ctx, styleWho(f.B), f.B.Tenant, f.B.Project, other.ID); e != nil {
		t.Fatal("owned second tenant abort")
	}
	if e := conn.QueryRow(ctx, `SELECT tenant_id,upload_id,cleanup_token,create_started,multipart_id FROM photo.claim_style_package_cleanup()`).Scan(&tenant, &id, &token, &started, &multipart); e != nil || tenant != f.B.Tenant || id != other.ID {
		t.Fatal("exact second owned cleanup ticket", e)
	}
	if e := stylePhysicalPurge(ctx, objects, tenant, id); e != nil {
		t.Fatal("second owned physical purge", e)
	}
	if e := conn.QueryRow(ctx, `SELECT photo.finish_style_package_cleanup($1,$2,$3)`, tenant, id, token).Scan(&ok); e != nil || !ok {
		t.Fatal("second owned cleanup finish", e)
	}
}

func styleMaintenancePool(t *testing.T, ctx context.Context, raw string) *pgxpool.Pool {
	t.Helper()
	var entry genuineCredential
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&entry) != nil || d.Decode(new(any)) != io.EOF || !regexp.MustCompile(`^editor_owned_[a-z0-9_]{16,48}$`).MatchString(entry.Login) {
		t.Fatal("dedicated maintenance credential required")
	}
	cfg, e := pgxpool.ParseConfig(entry.DSN)
	if e != nil || cfg.ConnConfig.User != entry.Login || cfg.ConnConfig.RuntimeParams["options"] != "-c role=photo_maintenance" || (cfg.ConnConfig.RuntimeParams["role"] != "" && cfg.ConnConfig.RuntimeParams["role"] != "photo_maintenance") {
		t.Fatal("generic owner SET ROLE maintenance refused")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	database.GuardApplicationRole(cfg)
	previous := cfg.AfterConnect
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		if e := previous(ctx, c); e != nil {
			return e
		}
		var ok bool
		if e := c.QueryRow(ctx, `SELECT session_user=$1 AND current_user='photo_maintenance'`, entry.Login).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("maintenance pool")
	}
	t.Cleanup(pool.Close)
	conn, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal("dedicated maintenance identity attestation")
	}
	conn.Release()
	return pool
}

// The object is genuinely created once, then its reply is lost at the owned
// service boundary. No ready or cleanup state is fabricated in SQL.
type styleLostCreate struct{ *objectstore.Store }

func (o styleLostCreate) BeginStylePackageMultipart(ctx context.Context, tenant, upload uuid.UUID, media string) (string, error) {
	_, e := o.Store.BeginStylePackageMultipart(ctx, tenant, upload, media)
	if e != nil {
		return "", e
	}
	return "", errors.New("owned fixture lost real Create reply")
}
func TestIntegrationStylePackageUnknownCreateRecovery(t *testing.T) {
	ctx, f, s, objects, maintenance, audit := styleHostileInputs(t)
	s.Objects = styleLostCreate{objects}
	file, e := os.Open(f.Path)
	if e != nil {
		t.Fatal("actual source")
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() {
		t.Fatal("real regular source")
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(file, styleupload.MaxBytes+1))
	if e != nil || n < 2 || n > styleupload.MaxBytes || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		t.Fatal("source SHA")
	}
	key := uuid.NewString()
	if _, e := s.Begin(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, key, styleupload.BeginRequest{Filename: filepath.Base(f.Path), Bytes: n, SHA256: f.SHA256}); e == nil {
		t.Fatal("lost real Create reply was accepted")
	}
	var id uuid.UUID
	var ioUntil time.Time
	if e := audit.QueryRow(ctx, `SELECT id,io_until FROM photo.style_package_uploads WHERE tenant_id=$1 AND project_id=$2 AND state='initializing' AND create_started AND multipart_id IS NULL`, f.A.Tenant, f.A.Project).Scan(&id, &ioUntil); e != nil {
		t.Fatal("durable unknown-create witness", e)
	}
	if _, e := s.Abort(ctx, styleWho(f.A), f.A.Tenant, f.A.Project, id); e != nil {
		t.Fatal("actual abort unknown create", e)
	}
	var count int
	if e := maintenance.QueryRow(ctx, `SELECT count(*) FROM photo.claim_style_package_cleanup()`).Scan(&count); e != nil || count != 0 {
		t.Fatal("cleanup crossed live IO horizon", e)
	}
	wait := time.Until(ioUntil) + time.Second
	if wait > 100*time.Second {
		t.Fatal("unexpected IO horizon")
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal("bounded IO horizon wait", ctx.Err())
		}
	}
	var tenant, upload, token uuid.UUID
	var started bool
	var multipart *string
	if e := maintenance.QueryRow(ctx, `SELECT tenant_id,upload_id,cleanup_token,create_started,multipart_id FROM photo.claim_style_package_cleanup()`).Scan(&tenant, &upload, &token, &started, &multipart); e != nil || tenant != f.A.Tenant || upload != id || !started || multipart != nil {
		t.Fatal("exact unknown-create claim", e)
	}
	var ok bool
	if e := maintenance.QueryRow(ctx, `SELECT photo.finish_style_package_cleanup($1,$2,$3)`, tenant, upload, token).Scan(&ok); e != nil || ok {
		t.Fatal("unresolved MPU released quota")
	}
	found, e := objects.DiscoverStylePackageMultipart(ctx, tenant, upload)
	if e != nil || found == "" {
		t.Fatal("actual sole Create MPU not discoverable", e)
	}
	if e := maintenance.QueryRow(ctx, `SELECT photo.bind_style_package_cleanup_multipart($1,$2,$3,$4)`, tenant, upload, token, found).Scan(&ok); e != nil || !ok {
		t.Fatal("discovered MPU fence binding", e)
	}
	if e := stylePhysicalPurge(ctx, objects, tenant, upload); e != nil {
		t.Fatal("real discovered MPU purge", e)
	}
	if e := maintenance.QueryRow(ctx, `SELECT photo.finish_style_package_cleanup($1,$2,$3)`, tenant, upload, token).Scan(&ok); e != nil || !ok {
		t.Fatal("proven physical cleanup refused", e)
	}
}

func stylePhysicalPurge(ctx context.Context, objects *objectstore.Store, tenant, upload uuid.UUID) error {
	bounded, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	return objects.PurgeStylePackage(bounded, tenant, upload)
}
