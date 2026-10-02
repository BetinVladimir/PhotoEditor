package upload

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

func TestIntegrationBeginReservesOnceAndResumesMultipart(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with migration77 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var user, tenantID, projectID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", sessionID, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Uploads','workspace') RETURNING id", tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	objects := new(multipartFake)
	service := Service{DB: api, Objects: objects}
	who := auth.Identity{UserID: user, SessionID: sessionID, Epoch: epoch}
	request := BeginRequest{ProjectID: projectID, Filename: "photo.jpg", Bytes: 1024, Format: "jpeg"}
	first, err := service.Begin(ctx, who, tenantID, "same-upload", request)
	if err != nil || first.State != "uploading" || first.Revision != "2" || objects.begins != 1 {
		t.Fatal("begin", first, objects.begins, err)
	}
	second, err := service.Begin(ctx, who, tenantID, "same-upload", request)
	if err != nil || second.ID != first.ID || objects.begins != 1 {
		t.Fatal("replay", second, objects.begins, err)
	}
	var reservedBytes, reservedFiles int64
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes,reserved_files FROM photo.tenant_usage WHERE tenant_id=$1", tenantID).Scan(&reservedBytes, &reservedFiles); err != nil || reservedBytes != 1024 || reservedFiles != 1 {
		t.Fatal("quota reservation", reservedBytes, reservedFiles, err)
	}
	accepted, err := service.Complete(ctx, who, tenantID, first.ID, "complete-upload", CompleteRequest{ExpectedSize: 1024, Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: "etag-1"}}})
	if err != nil || accepted.JobID != first.ID || accepted.Status != "queued" {
		t.Fatal("complete intent", accepted, err)
	}
	replayed, err := service.Complete(ctx, who, tenantID, first.ID, "complete-upload", CompleteRequest{ExpectedSize: 1024, Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: "etag-1"}}})
	if err != nil || replayed != accepted {
		t.Fatal("complete replay", replayed, err)
	}
	var state string
	var parts int
	if err = admin.QueryRow(ctx, "SELECT state,(SELECT count(*) FROM photo.upload_parts WHERE tenant_id=$2 AND upload_session_id=$1) FROM photo.upload_sessions WHERE id=$1", first.ID, tenantID).Scan(&state, &parts); err != nil || state != "completing" || parts != 1 {
		t.Fatal("completion persistence", state, parts, err)
	}
	toAbort, err := service.Begin(ctx, who, tenantID, "abort-upload", BeginRequest{ProjectID: projectID, Filename: "other.jpg", Bytes: 2048, Format: "jpeg"})
	if err != nil {
		t.Fatal("begin abort fixture", err)
	}
	aborted, err := service.Abort(ctx, who, tenantID, toAbort.ID)
	if err != nil || aborted.State != "aborted" {
		t.Fatal("abort", aborted, err)
	}
	aborted, err = service.Abort(ctx, who, tenantID, toAbort.ID)
	if err != nil || aborted.State != "aborted" {
		t.Fatal("abort replay", aborted, err)
	}
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes,reserved_files FROM photo.tenant_usage WHERE tenant_id=$1", tenantID).Scan(&reservedBytes, &reservedFiles); err != nil || reservedBytes != 1024 || reservedFiles != 1 {
		t.Fatal("abort quota release", reservedBytes, reservedFiles, err)
	}
}

func TestIntegrationConcurrentQuotaReservationHasOneWinner(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var user, tenantID, projectID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.tenants SET quotas=jsonb_set(jsonb_set(quotas,'{storage_bytes}','1500'),'{source_files}','1') WHERE id=$1`, tenantID); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", sessionID, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Quota race','workspace') RETURNING id", tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	objects := new(multipartFake)
	service := Service{DB: api, Objects: objects}
	who := auth.Identity{UserID: user, SessionID: sessionID, Epoch: epoch}
	request := BeginRequest{ProjectID: projectID, Filename: "photo.jpg", Bytes: 1000, Format: "jpeg"}
	type result struct {
		session Session
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for index := 0; index < 2; index++ {
		go func(key string) {
			ready.Done()
			<-start
			session, beginErr := service.Begin(ctx, who, tenantID, key, request)
			results <- result{session, beginErr}
		}("quota-race-" + string(rune('a'+index)))
	}
	ready.Wait()
	close(start)
	successes := make([]Session, 0, 1)
	failures := 0
	for index := 0; index < 2; index++ {
		outcome := <-results
		if outcome.err == nil {
			successes = append(successes, outcome.session)
		} else {
			failures++
		}
	}
	if len(successes) != 1 || failures != 1 {
		t.Fatalf("quota race outcomes: successes=%d failures=%d", len(successes), failures)
	}
	objects.mu.Lock()
	begins := objects.begins
	objects.mu.Unlock()
	if begins != 1 {
		t.Fatalf("storage multipart side effects=%d", begins)
	}
	var reservedBytes, reservedFiles int64
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes,reserved_files FROM photo.tenant_usage WHERE tenant_id=$1", tenantID).Scan(&reservedBytes, &reservedFiles); err != nil {
		t.Fatal(err)
	}
	if reservedBytes != 1000 || reservedFiles != 1 {
		t.Fatalf("quota counters bytes=%d files=%d", reservedBytes, reservedFiles)
	}
	if _, err = service.Abort(ctx, who, tenantID, successes[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT reserved_bytes,reserved_files FROM photo.tenant_usage WHERE tenant_id=$1", tenantID).Scan(&reservedBytes, &reservedFiles); err != nil || reservedBytes != 0 || reservedFiles != 0 {
		t.Fatalf("quota release bytes=%d files=%d err=%v", reservedBytes, reservedFiles, err)
	}
}
