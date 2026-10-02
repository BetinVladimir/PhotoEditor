package upload

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2capture"
)

func TestIntegrationCaptureDeviceUpload(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	scan := func(q string, dst any, args ...any) {
		t.Helper()
		if err := admin.QueryRow(ctx, q, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var actor, tenant, project, collection, session, device uuid.UUID
	scan(`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`, &actor, uuid.NewString()+"@device.test")
	scan(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`, &tenant, actor)
	scan(`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Device upload') RETURNING id`, &project, tenant)
	scan(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Frames') RETURNING id`, &collection, tenant, project)
	scan(`INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,policy,capture_policy,consent_epoch) VALUES($1,$2,$3,'Studio','open','{}','{}',1) RETURNING id`, &session, tenant, project, collection)
	scan(`INSERT INTO photo.capture_devices(tenant_id,project_id,label,public_key,created_by) VALUES($1,$2,'Camera',$3,$4) RETURNING id`, &device, tenant, project, strings.Repeat("A", 43), actor)
	principal := v2capture.DevicePrincipal{TenantID: tenant, ProjectID: project, DeviceID: device, CredentialEpoch: 1}
	objects := new(multipartFake)
	service := Service{DB: api, Objects: objects}
	sha := strings.Repeat("a", 64)
	request := DeviceBeginRequest{SessionID: session, BeginRequest: BeginRequest{ProjectID: project, CollectionID: &collection, Filename: "frame.jpg", Bytes: 1024, Format: "jpg", SHA256: &sha}}
	first, err := service.BeginDevice(ctx, principal, "device-begin", request)
	if err != nil || first.State != "uploading" || objects.begins != 1 {
		t.Fatal("device begin", first, err)
	}
	replay, err := service.BeginDevice(ctx, principal, "device-begin", request)
	if err != nil || replay.ID != first.ID || objects.begins != 1 {
		t.Fatal("device replay", replay, err)
	}
	var bytes, files int64
	if err = admin.QueryRow(ctx, `SELECT reserved_bytes,reserved_files FROM photo.tenant_usage WHERE tenant_id=$1`, tenant).Scan(&bytes, &files); err != nil || bytes != 1024 || files != 1 {
		t.Fatal("duplicate quota", bytes, files, err)
	}
	for _, numbers := range [][]int{{0}, {2}, {1, 1}} {
		if _, err = service.SignDeviceParts(ctx, principal, first.ID, PartRequest{PartNumbers: numbers}); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatal("invalid multipart number signed", numbers, err)
		}
	}
	if objects.signs != 0 {
		t.Fatal("invalid request reached signer")
	}
	urls, err := service.SignDeviceParts(ctx, principal, first.ID, PartRequest{PartNumbers: []int{1}})
	if err != nil || len(urls.Items) != 1 || objects.signs != 1 {
		t.Fatal("valid part signing", urls, err)
	}
	complete := CompleteRequest{ExpectedSize: 1024, Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: "part-one"}}}
	accepted, err := service.CompleteDevice(ctx, principal, first.ID, "device-complete", complete)
	if err != nil || accepted.Status != "queued" {
		t.Fatal("device complete", accepted, err)
	}
	repeated, err := service.CompleteDevice(ctx, principal, first.ID, "device-complete", complete)
	if err != nil || repeated != accepted {
		t.Fatal("device complete replay", repeated, err)
	}
	var partCount int
	scan(`SELECT count(*) FROM photo.upload_parts WHERE tenant_id=$1 AND upload_session_id=$2`, &partCount, tenant, first.ID)
	if partCount != 1 {
		t.Fatal("completion replay duplicated parts", partCount)
	}
	exec(`UPDATE photo.shoot_sessions SET state='closed' WHERE id=$1`, session)
	if _, err = service.BeginDevice(ctx, principal, "closed-begin", request); domain.AsError(err).Code != "NOT_FOUND" {
		t.Fatal("closed session admitted new upload", err)
	}
	exec(`UPDATE photo.shoot_sessions SET state='open' WHERE id=$1`, session)
	exec(`UPDATE photo.capture_devices SET credential_epoch=credential_epoch+1 WHERE id=$1`, device)
	if _, err = service.BeginDevice(ctx, principal, "device-begin", request); domain.AsError(err).Code != "INVALID_CREDENTIALS" {
		t.Fatal("replay bypassed credential rotation", err)
	}
	if _, err = service.GetDevice(ctx, principal, first.ID); domain.AsError(err).Code != "INVALID_CREDENTIALS" {
		t.Fatal("old epoch read upload", err)
	}
	if _, err = service.CompleteDevice(ctx, principal, first.ID, "device-complete", complete); domain.AsError(err).Code != "INVALID_CREDENTIALS" {
		t.Fatal("completion replay bypassed credential rotation", err)
	}
	principal.CredentialEpoch = 2
	if _, err = service.GetDevice(ctx, principal, first.ID); domain.AsError(err).Code != "NOT_FOUND" {
		t.Fatal("new epoch adopted old upload", err)
	}
}
