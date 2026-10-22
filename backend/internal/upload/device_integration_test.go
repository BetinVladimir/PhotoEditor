package upload

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/rawdevelopment"
	"photoeditor/backend/internal/v2capture"
	"photoeditor/backend/internal/v2qualification"
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
	service.Objects = legacyDeviceMultipartStore{MultipartStore: objects}
	if _, err := service.BeginDevice(ctx, principal, "unsupported-starter", request); err == nil || err.Error() != "capture multipart starter unavailable" {
		t.Fatal("missing capture starter accepted", err)
	}
	var preflightRows int
	scan(`SELECT count(*) FROM photo.upload_sessions WHERE tenant_id=$1 AND filename='frame.jpg'`, &preflightRows, tenant)
	if preflightRows != 0 || objects.begins != 0 {
		t.Fatal("missing capture starter persisted a reservation or called storage", preflightRows, objects.begins)
	}
	service.Objects = objects
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
	// The real device admission SHARE lock prevents revocation during signing;
	// it does not stop time advancing while the object store returns a URL.
	clock := time.Now()
	service.Now = func() time.Time { return clock }
	barrier := &signingBarrierStore{afterSign: func() { clock = first.ExpiresAt.Add(time.Second) }}
	service.Objects = barrier
	if signed, signErr := service.SignDeviceParts(ctx, principal, first.ID, PartRequest{PartNumbers: []int{1}}); domain.AsError(signErr).Code != "UPLOAD_STATE_INVALID" || len(signed.Items) != 0 {
		t.Fatal("expired device session disclosed signed bearer", signed, signErr)
	}
	service.Now = nil
	signCtx, signCancel := context.WithCancel(ctx)
	barrier.afterSign = signCancel
	if signed, signErr := service.SignDeviceParts(signCtx, principal, first.ID, PartRequest{PartNumbers: []int{1}}); !errors.Is(signErr, context.Canceled) || len(signed.Items) != 0 {
		t.Fatal("cancelled device signing disclosed bearer", signed, signErr)
	}
	service.Objects = objects
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
	// RAW admission rollback/replay is exercised with a named synthetic
	// qualification adapter. Native/report SQL qualification is a separate gate.
	rawRequest := request
	rawRequest.Filename = "frame.nef"
	rawRequest.Format = "nef"
	rawRequest.RAWDevelopment = &rawdevelopment.Policy{SchemaVersion: 1, Demosaic: "ahd", WBSource: "camera", ExposureEV: 1, HighlightPolicy: "blend", DecoderVersion: "rawpy-0.27.1/libraw-0.21.4"}
	beforeBegins := objects.begins
	if _, err = service.BeginDevice(ctx, principal, "raw-missing", rawRequest); domain.AsError(err).Code != "QUALIFICATION_UNAVAILABLE" {
		t.Fatal("RAW without installed source accepted", err)
	}
	if objects.begins != beforeBegins {
		t.Fatal("unqualified RAW reached multipart storage")
	}
	var failedRows int
	scan(`SELECT count(*) FROM photo.upload_sessions WHERE tenant_id=$1 AND filename='frame.nef'`, &failedRows, tenant)
	if failedRows != 0 {
		t.Fatal("unqualified RAW persisted admission")
	}
	adapter := &deviceRAWAdmissionFixture{principal: principal, shoot: session, collection: collection}
	service.RAWAdmission = adapter
	rawUpload, err := service.BeginDevice(ctx, principal, "raw-begin", rawRequest)
	if err != nil || rawUpload.ID == uuid.Nil {
		t.Fatal("RAW admission fixture", err)
	}
	rawReplay, err := service.BeginDevice(ctx, principal, "raw-begin", rawRequest)
	if err != nil || rawReplay.ID != rawUpload.ID || adapter.calls != 2 {
		t.Fatal("RAW replay skipped qualification", err)
	}
	beforeBegins = objects.begins
	adapter.revoked = true
	if _, err = service.BeginDevice(ctx, principal, "raw-begin", rawRequest); !errors.Is(err, v2qualification.ErrUnqualified) {
		t.Fatal("RAW replay bypassed revoked qualification", err)
	}
	if objects.begins != beforeBegins {
		t.Fatal("revoked RAW replay touched multipart storage")
	}
	var rawRows, rawReservations int
	scan(`SELECT count(*) FROM photo.upload_sessions WHERE tenant_id=$1 AND filename='frame.nef'`, &rawRows, tenant)
	scan(`SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_id=$2`, &rawReservations, tenant, rawUpload.ID)
	if rawRows != 1 || rawReservations != 1 {
		t.Fatal("RAW replay duplicated admission", rawRows, rawReservations)
	}
	service.RAWAdmission = nil
	// A lost object-store Begin response may mean a remote multipart exists.
	// The persisted writer fence must prevent a second external Begin.
	objects.beginErr = errors.New("lost begin response")
	ambiguous := request
	ambiguous.Filename = "ambiguous.jpg"
	if _, err = service.BeginDevice(ctx, principal, "ambiguous-begin", ambiguous); err == nil {
		t.Fatal("ambiguous object-store begin accepted")
	}
	beginCount := objects.begins
	objects.beginErr = nil
	if _, err = service.BeginDevice(ctx, principal, "ambiguous-begin", ambiguous); domain.AsError(err).Code != "CAPTURE_BEGIN_RECONCILIATION_REQUIRED" {
		t.Fatal("ambiguous begin replay bypassed reconciliation fence", err)
	}
	if objects.begins != beginCount {
		t.Fatal("ambiguous begin replay created a second remote writer")
	}
	var ambiguousID, claim uuid.UUID
	scan(`SELECT id,device_begin_token FROM photo.upload_sessions
		WHERE tenant_id=$1 AND filename='ambiguous.jpg'`, &ambiguousID, &claim, tenant)
	var reconciled bool
	scan(`SELECT photo.reconcile_device_begin($1,$2,$3,'bind','multipart-token',true,$4)`,
		&reconciled, tenant, ambiguousID, uuid.New(), strings.Repeat("a", 64))
	if reconciled {
		t.Fatal("wrong writer claim reconciled")
	}
	scan(`SELECT photo.reconcile_device_begin($1,$2,$3,'bind','multipart-token',true,$4)`,
		&reconciled, tenant, ambiguousID, claim, strings.Repeat("a", 64))
	if !reconciled {
		t.Fatal("operator could not bind observed writer")
	}
	recovered, err := service.BeginDevice(ctx, principal, "ambiguous-begin", ambiguous)
	if err != nil || recovered.ID != ambiguousID || recovered.State != "uploading" || objects.begins != beginCount {
		t.Fatal("reconciled begin replay created new writer", recovered, err)
	}
	objects.beginID = "bad\nreceipt"
	abortCount := objects.aborts
	invalidReceipt := request
	invalidReceipt.Filename = "bad-receipt.jpg"
	if _, err = service.BeginDevice(ctx, principal, "bad-receipt-begin", invalidReceipt); !errors.Is(err, objectstore.ErrIntegrity) {
		t.Fatal("malformed storage receipt was attached", err)
	}
	beginCount = objects.begins
	objects.beginID = ""
	if _, err = service.BeginDevice(ctx, principal, "bad-receipt-begin", invalidReceipt); domain.AsError(err).Code != "CAPTURE_BEGIN_RECONCILIATION_REQUIRED" || objects.begins != beginCount {
		t.Fatal("malformed receipt replay started another writer", err)
	}
	if objects.aborts != abortCount {
		t.Fatal("ambiguous capture writer was aborted without reconciliation")
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

// Deliberately exposes only the legacy multipart interface; its embedded
// implementation must not promote BeginCaptureMultipart into the method set.
type legacyDeviceMultipartStore struct{ MultipartStore }

// Explicit fixture for upload transaction/replay tests, never production wiring.
type deviceRAWAdmissionFixture struct {
	principal         v2capture.DevicePrincipal
	shoot, collection uuid.UUID
	calls             int
	revoked           bool
}

func (a *deviceRAWAdmissionFixture) Admit(ctx context.Context, tx rawdevelopment.ReportRows, tenantID, projectID, actor, session uuid.UUID, p *rawdevelopment.Policy) (*rawdevelopment.Policy, error) {
	return nil, v2qualification.ErrUnqualified
}
func (a *deviceRAWAdmissionFixture) AdmitDevice(ctx context.Context, tx rawdevelopment.ReportRows, principal v2capture.DevicePrincipal, shoot, collection uuid.UUID, p *rawdevelopment.Policy) (*rawdevelopment.Policy, error) {
	a.calls++
	if principal != a.principal || shoot != a.shoot || collection != a.collection || a.revoked {
		return nil, v2qualification.ErrUnqualified
	}
	// Verify the adapter is inside the authenticated transaction, not invoked
	// through an autocommit pool or before the tenant context is installed.
	var tenantSetting string
	if err := tx.QueryRow(ctx, `SELECT current_setting('app.tenant_id',true)`).Scan(&tenantSetting); err != nil {
		return nil, err
	}
	if tenantSetting != principal.TenantID.String() {
		return nil, v2qualification.ErrUnqualified
	}
	owned := *p
	return &owned, nil
}
