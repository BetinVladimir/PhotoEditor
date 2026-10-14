package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"strings"
	"testing"
)

type freezeS3Probe struct {
	*objectstore.Store
	reads, puts, recoveries int
	denyReads               bool
	afterPut                func()
}

func (s *freezeS3Probe) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	s.reads++
	if s.denyReads {
		return nil, objectstore.ErrUnavailable
	}
	return s.Store.Open(ctx, obj)
}
func (s *freezeS3Probe) PutImmutable(ctx context.Context, key string, r io.Reader, n int64, sha, media string) (objectstore.Object, error) {
	s.puts++
	obj, err := s.Store.PutImmutable(ctx, key, r, n, sha, media)
	if err == nil && s.afterPut != nil {
		s.afterPut()
	}
	return obj, err
}
func (s *freezeS3Probe) RecoverImmutable(ctx context.Context, key string, n int64, sha, media string) (objectstore.Object, error) {
	s.recoveries++
	return s.Store.RecoverImmutable(ctx, key, n, sha, media)
}

func TestIntegrationRenderFreezeS3Recovery(t *testing.T) {
	raw, dsn, configRaw := os.Getenv("PHOTO_RENDER_FREEZE_FRESH"), os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_RENDER_FREEZE_S3_CONFIG")
	if raw == "" || dsn == "" || configRaw == "" {
		t.Skip("isolated SQL/S3 fixture required")
	}
	var f struct {
		Tenant, Actor, Session, Asset uuid.UUID
		Request                       RenderFreezeRequest
	}
	var config objectstore.Config
	if json.Unmarshal([]byte(raw), &f) != nil || json.Unmarshal([]byte(configRaw), &config) != nil {
		t.Fatal("invalid isolated fixture")
	}
	ctx := context.Background()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid isolated database")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	defer api.Close()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	defer admin.Close()
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	if err := admin.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", f.Actor).Scan(&who.Epoch); err != nil {
		t.Fatal("fixture identity unavailable")
	}
	store, err := objectstore.New(config)
	if err != nil {
		t.Fatal("isolated S3 unavailable")
	}
	probe := &freezeS3Probe{Store: store}
	q := &freezeQualifiedFixture{q: RenderQualification{ModelSHA: strings.Repeat("a", 64), RuntimeSHA: strings.Repeat("b", 64), Backend: "cpu", DeviceSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64)}}
	service := SQLRenderFreezeService{Drafts: v2draft.Service{DB: api, Objects: probe, Bucket: config.Bucket}, Qualification: q}
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	probe.afterPut = cancel
	_, err = service.Freeze(cancelled, who, f.Tenant, f.Asset, "s3-recovery", f.Request)
	if err == nil || probe.puts != 1 {
		t.Fatalf("S3 cancellation boundary not reached: code=%s type=%T reads=%d puts=%d", domain.AsError(err).Code, err, probe.reads, probe.puts)
	}
	reads, calls := probe.reads, q.calls
	probe.afterPut = nil
	probe.denyReads = true
	service.Qualification = nil
	response, err := service.Freeze(ctx, who, f.Tenant, f.Asset, "s3-recovery", f.Request)
	if err != nil || response.Status != 200 || response.Replayed || probe.reads != reads || q.calls != calls || probe.recoveries != 1 {
		t.Fatal("immutable S3 recovery failed", err)
	}
	id, _ := RenderFreezeDocumentID(f.Tenant, f.Actor, f.Asset, "s3-recovery", f.Request)
	result, err := DecodeRenderFreezeResult(response.Body, id, f.Request)
	if err != nil {
		t.Fatal("invalid recovered result")
	}
	var obj objectstore.Object
	if err := admin.QueryRow(ctx, "SELECT object_key,object_version,bytes,sha256,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", f.Tenant, result.Recipe.ArtifactID).Scan(&obj.Key, &obj.VersionID, &obj.Bytes, &obj.SHA256, &obj.ContentType); err != nil {
		t.Fatal("registered S3 object missing")
	}
	reader, err := store.Open(ctx, obj)
	if err != nil {
		t.Fatal("registered immutable S3 version unavailable")
	}
	body, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal("registered S3 object unreadable")
	}
	frozen, err := v2draft.DecodeFrozenRenderRecipe(body)
	if err != nil || frozen.Scalars.Source != result.Source || frozen.Scalars.EffectiveDelta.EV != .1 {
		t.Fatal("recovered S3 wire invalid")
	}
	puts, recoveries := probe.puts, probe.recoveries
	replay, err := service.Freeze(ctx, who, f.Tenant, f.Asset, "s3-recovery", f.Request)
	if err != nil || !replay.Replayed || !bytes.Equal(response.Body, replay.Body) || probe.puts != puts || probe.recoveries != recoveries || probe.reads != reads {
		t.Fatal("completed S3 replay performed IO", err)
	}
	service.Qualification = q
	probe.denyReads = false
	if _, err := admin.Exec(ctx, `CREATE FUNCTION photo.test_s3_freeze_abort() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.render_registered_at IS NOT NULL THEN RAISE EXCEPTION 'controlled S3 final registration failure';END IF;RETURN NEW;END $$; CREATE TRIGGER test_s3_freeze_abort BEFORE UPDATE ON photo.v2_draft_uploads FOR EACH ROW EXECUTE FUNCTION photo.test_s3_freeze_abort()`); err != nil {
		t.Fatal("isolated failure trigger unavailable")
	}
	_, err = service.Freeze(ctx, who, f.Tenant, f.Asset, "s3-registration-recovery", f.Request)
	if err == nil {
		t.Fatal("final registration failure missing")
	}
	failedID, _ := RenderFreezeDocumentID(f.Tenant, f.Actor, f.Asset, "s3-registration-recovery", f.Request)
	var before objectstore.Object
	var ready, registered bool
	if err := admin.QueryRow(ctx, `SELECT a.object_key,a.object_version,a.bytes,a.sha256,a.media_type,u.state='ready',u.render_registered_at IS NOT NULL FROM photo.v2_draft_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id WHERE u.tenant_id=$1 AND u.document_id=$2`, f.Tenant, failedID).Scan(&before.Key, &before.VersionID, &before.Bytes, &before.SHA256, &before.ContentType, &ready, &registered); err != nil || !ready || registered {
		t.Fatal("failed final registration changed ready upload")
	}
	if _, err := admin.Exec(ctx, `DROP TRIGGER test_s3_freeze_abort ON photo.v2_draft_uploads; DROP FUNCTION photo.test_s3_freeze_abort()`); err != nil {
		t.Fatal("isolated failure trigger cleanup failed")
	}
	reads, puts, recoveries = probe.reads, probe.puts, probe.recoveries
	service.Qualification = nil
	probe.denyReads = true
	recovered, err := service.Freeze(ctx, who, f.Tenant, f.Asset, "s3-registration-recovery", f.Request)
	if err != nil || recovered.Status != 200 || recovered.Replayed || probe.reads != reads || probe.puts != puts || probe.recoveries != recoveries {
		t.Fatal("ready S3 registration recovery performed IO", err)
	}
	recoveredResult, err := DecodeRenderFreezeResult(recovered.Body, failedID, f.Request)
	if err != nil || recoveredResult.Recipe.SHA256 != before.SHA256 {
		t.Fatal("ready S3 recovery replaced recipe")
	}
	var version string
	if err := admin.QueryRow(ctx, `SELECT object_version FROM photo.artifacts WHERE tenant_id=$1 AND id=$2`, f.Tenant, recoveredResult.Recipe.ArtifactID).Scan(&version); err != nil || version != before.VersionID {
		t.Fatal("ready S3 recovery replaced object version")
	}
	if err := store.Check(ctx, before); err != nil {
		t.Fatal("original S3 object failed integrity verification")
	}

}
