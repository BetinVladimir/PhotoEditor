package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

type freezeDocumentStore struct {
	bodies      map[string][]byte
	reads, puts int
	failReads   bool
	afterPut    func()
}

func (s *freezeDocumentStore) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	s.reads++
	if s.failReads {
		return nil, objectstore.ErrUnavailable
	}
	body, ok := s.bodies[obj.Key]
	if !ok {
		return nil, objectstore.ErrMissing
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}
func (s *freezeDocumentStore) PutImmutable(_ context.Context, key string, reader io.Reader, length int64, digest, media string) (objectstore.Object, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return objectstore.Object{}, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != length || hex.EncodeToString(sum[:]) != digest {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	s.bodies[key] = body
	s.puts++
	if s.afterPut != nil {
		s.afterPut()
	}
	return objectstore.Object{Key: key, Bytes: length, SHA256: digest, VersionID: "v1", ContentType: media}, nil
}
func (s *freezeDocumentStore) RecoverImmutable(_ context.Context, key string, length int64, digest, media string) (objectstore.Object, error) {
	body, ok := s.bodies[key]
	if !ok {
		return objectstore.Object{}, objectstore.ErrMissing
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != length || hex.EncodeToString(sum[:]) != digest {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	return objectstore.Object{Key: key, Bytes: length, SHA256: digest, VersionID: "v1", ContentType: media}, nil
}

type freezeQualifiedFixture struct {
	calls int
	q     RenderQualification
}

func (q *freezeQualifiedFixture) Resolve(context.Context, auth.Identity, uuid.UUID, uuid.UUID) (RenderQualification, error) {
	q.calls++
	return q.q, nil
}

func TestIntegrationRenderFreezeFreshDocuments(t *testing.T) {
	raw, dsn := os.Getenv("PHOTO_RENDER_FREEZE_FRESH"), os.Getenv("PHOTO_TEST_DATABASE_URL")
	if raw == "" || dsn == "" {
		t.Skip("isolated full document fixture required")
	}
	var f struct {
		Tenant, Actor, Session, Asset, Stream uuid.UUID
		Request                               RenderFreezeRequest
		Objects                               map[string]string
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("invalid full freeze fixture")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("isolated DB unavailable")
	}
	defer admin.Close()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid isolated DB")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("API DB unavailable")
	}
	defer api.Close()
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	if err := admin.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", f.Actor).Scan(&who.Epoch); err != nil {
		t.Fatal(err)
	}
	store := &freezeDocumentStore{bodies: map[string][]byte{}}
	for key, body := range f.Objects {
		store.bodies[key] = []byte(body)
	}
	q := &freezeQualifiedFixture{q: RenderQualification{ModelSHA: strings.Repeat("a", 64), RuntimeSHA: strings.Repeat("b", 64), Backend: "cpu", DeviceSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64)}}
	service := SQLRenderFreezeService{Drafts: v2draft.Service{DB: api, Objects: store, Bucket: "test"}, Qualification: q}
	response, err := service.Freeze(ctx, who, f.Tenant, f.Asset, "fresh-freeze", f.Request)
	if err != nil || response.Status != 200 || response.Replayed {
		t.Fatal("fresh freeze failed", err)
	}
	id, _ := RenderFreezeDocumentID(f.Tenant, f.Actor, f.Asset, "fresh-freeze", f.Request)
	result, err := DecodeRenderFreezeResult(response.Body, id, f.Request)
	if err != nil {
		t.Fatal(err)
	}
	var projectID uuid.UUID
	if err := admin.QueryRow(ctx, "SELECT project_id FROM photo.assets WHERE tenant_id=$1 AND id=$2", f.Tenant, f.Asset).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	// Unsupported policy must fail before calibration/object IO or durable draft creation.
	schedulerConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	schedulerConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, schedulerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	submission := SQLRenderSubmissionService{Factory: SQLRenderBatchFactory{Scheduler: scheduler}, Timeout: time.Minute}
	deniedIntent := RenderBatchSubmissionRequest{SnapshotID: uuid.New(), PolicySHA: strings.Repeat("f", 64), Selection: []RenderSelectionItem{{Binding: result.Source, Recipe: result.Recipe}}}
	deniedBatch, _, err := CompileRenderSubmissionIdentity(f.Tenant, f.Actor, projectID, "unsupported-policy", deniedIntent)
	if err != nil {
		t.Fatal(err)
	}
	readsBefore, putsBefore := store.reads, store.puts
	if _, err = submission.Queue(ctx, who, f.Tenant, projectID, "unsupported-policy", deniedIntent); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("unsupported fresh policy accepted", err)
	}
	var deniedRows int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", f.Tenant, deniedBatch).Scan(&deniedRows); err != nil || deniedRows != 0 || store.reads != readsBefore || store.puts != putsBefore {
		t.Fatal("unsupported policy caused mutation or storage IO", err)
	}
	snapshotService := SQLRenderInputSnapshotService{DB: api}
	inventory := []v2input.SessionVersionBinding{result.Source}
	snapshot, err := snapshotService.Create(ctx, who, f.Tenant, projectID, "render-inventory", inventory)
	if err != nil || snapshot.Status != 201 || snapshot.Replayed {
		t.Fatal("V2 render inventory creation failed", err)
	}
	snapshotReplay, err := snapshotService.Create(ctx, who, f.Tenant, projectID, "render-inventory", inventory)
	if err != nil || !snapshotReplay.Replayed || !bytes.Equal(snapshot.Body, snapshotReplay.Body) {
		t.Fatal("V2 render inventory replay failed", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, f.Tenant, result.Source.Source.ArtifactID); err != nil {
		t.Fatal("source expiry fixture", err)
	}
	_, err = snapshotService.Create(ctx, who, f.Tenant, projectID, "render-inventory", inventory)
	if err == nil || domain.AsError(err).Code != "SNAPSHOT_EXPIRED" {
		t.Fatal("saved inventory bypassed expired source", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=$2`, f.Tenant, result.Source.Source.ArtifactID); err != nil {
		t.Fatal("source restore fixture", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, f.Tenant, f.Asset); err != nil {
		t.Fatal("asset revoke fixture", err)
	}
	_, err = snapshotService.Create(ctx, who, f.Tenant, projectID, "render-inventory", inventory)
	if err == nil || domain.AsError(err).Code != "SNAPSHOT_EXPIRED" {
		t.Fatal("saved inventory bypassed deleted asset", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE photo.assets SET deleted_at=NULL WHERE tenant_id=$1 AND id=$2`, f.Tenant, f.Asset); err != nil {
		t.Fatal("asset restore fixture", err)
	}
	restored, err := snapshotService.Create(ctx, who, f.Tenant, projectID, "render-inventory", inventory)
	if err != nil || !restored.Replayed || !bytes.Equal(restored.Body, snapshot.Body) {
		t.Fatal("scope denial changed saved inventory response", err)
	}
	failedSnapshotID, _ := RenderInputSnapshotID(f.Tenant, f.Actor, projectID, "wrong-render-inventory")
	wrong := append([]v2input.SessionVersionBinding(nil), inventory...)
	wrong[0].Source.SHA256 = strings.Repeat("f", 64)
	_, err = snapshotService.Create(ctx, who, f.Tenant, projectID, "render-inventory", wrong)
	if err == nil || domain.AsError(err).Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("inventory request conflict missing", err)
	}
	_, err = snapshotService.Create(ctx, who, f.Tenant, projectID, "wrong-render-inventory", wrong)
	if err == nil || domain.AsError(err).Code != "SNAPSHOT_EXPIRED" {
		t.Fatal("unavailable source inventory accepted", err)
	}
	var leftovers int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key='wrong-render-inventory'`, f.Tenant, f.Actor, "Render.InputSnapshot."+projectID.String()).Scan(&leftovers); err != nil || leftovers != 0 {
		t.Fatal("failed inventory left idempotency state", err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM photo.snapshots WHERE tenant_id=$1 AND id=$2`, f.Tenant, failedSnapshotID).Scan(&leftovers); err != nil || leftovers != 0 {
		t.Fatal("failed inventory left snapshot state", err)
	}
	var key string
	if err := admin.QueryRow(ctx, "SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", f.Tenant, result.Recipe.ArtifactID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	frozen, err := v2draft.DecodeFrozenRenderRecipe(store.bodies[key])
	if err != nil || frozen.Scalars.EffectiveDelta.EV != .1 || frozen.Scalars.Source != result.Source {
		t.Fatal("frozen scalar/source wire", err)
	}
	assertCompetingFreezePins(t, ctx, admin, service, who, f.Tenant, f.Asset, f.Request, store.bodies[key], frozen)
	err = tenant.WithTenant(ctx, api, who, f.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		if err := validateFrozenLearningScope(ctx, tx, f.Tenant, f.Asset, who, result.Recipe, frozen); err != nil {
			return err
		}
		missing := frozen
		missing.Learning = &v2draft.FrozenRenderLearning{VersionID: uuid.New(), Model: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("1", 64)}, Descriptor: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("2", 64)}, AnalysisID: uuid.New(), Ordinal: 0}
		err := validateFrozenLearningScope(ctx, tx, f.Tenant, f.Asset, who, result.Recipe, missing)
		if err == nil || domain.AsError(err).Code != "SNAPSHOT_EXPIRED" {
			t.Fatal("missing learning authority accepted", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal("learning scope guard", err)
	}

	expected := sha256.Sum256(store.bodies["draft"])
	if frozen.RecipeSHA256 != hex.EncodeToString(expected[:]) || !bytes.Equal(frozen.Recipe, store.bodies["draft"]) {
		t.Fatal("authored composition lost")
	}
	if path := os.Getenv("PHOTO_RENDER_FREEZE_RESULT"); path != "" {
		if err := os.WriteFile(path, store.bodies[key], 0600); err != nil {
			t.Fatal("cross-language recipe output", err)
		}
	}
	reads, puts, calls := store.reads, store.puts, q.calls
	store.failReads = true
	q.q.ModelSHA = strings.Repeat("f", 64)
	replay, err := service.Freeze(ctx, who, f.Tenant, f.Asset, "fresh-freeze", f.Request)
	if err != nil || !replay.Replayed || !bytes.Equal(response.Body, replay.Body) || store.reads != reads || store.puts != puts || q.calls != calls {
		t.Fatal("completed replay read objects/qualification", err)
	}
	store.failReads = false
	q.q.ModelSHA = strings.Repeat("a", 64)
	cancelled, cancel := context.WithCancel(ctx)
	store.afterPut = cancel
	_, err = service.Freeze(cancelled, who, f.Tenant, f.Asset, "interrupted-freeze", f.Request)
	if err == nil {
		t.Fatal("cancelled publication completed")
	}
	store.afterPut = nil
	var savedPending []byte
	if err := admin.QueryRow(ctx, `SELECT response_body FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key='interrupted-freeze'`, f.Tenant, f.Actor, "Render.Freeze."+f.Asset.String()).Scan(&savedPending); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE photo.idempotency_records SET response_body='{}'::jsonb WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key='interrupted-freeze'`, f.Tenant, f.Actor, "Render.Freeze."+f.Asset.String()); err != nil {
		t.Fatal(err)
	}
	_, err = service.Freeze(ctx, who, f.Tenant, f.Asset, "interrupted-freeze", f.Request)
	if err == nil || domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatal("malformed pending snapshot accepted", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE photo.idempotency_records SET response_body=$4,expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key='interrupted-freeze'`, f.Tenant, f.Actor, "Render.Freeze."+f.Asset.String(), savedPending); err != nil {
		t.Fatal(err)
	}
	_, err = service.Freeze(ctx, who, f.Tenant, f.Asset, "interrupted-freeze", f.Request)
	if err == nil || domain.AsError(err).Code != "SNAPSHOT_EXPIRED" {
		t.Fatal("expired pending snapshot accepted", err)
	}
	if _, err := admin.Exec(ctx, `UPDATE photo.idempotency_records SET expires_at=clock_timestamp()+interval '24 hours' WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key='interrupted-freeze'`, f.Tenant, f.Actor, "Render.Freeze."+f.Asset.String()); err != nil {
		t.Fatal(err)
	}
	reads = store.reads
	service.Qualification = nil
	store.failReads = true
	recovered, err := service.Freeze(ctx, who, f.Tenant, f.Asset, "interrupted-freeze", f.Request)
	if err != nil || recovered.Status != 200 || recovered.Replayed || store.reads != reads {
		t.Fatal("pending snapshot recovery failed", err)
	}
	recoveredID, _ := RenderFreezeDocumentID(f.Tenant, f.Actor, f.Asset, "interrupted-freeze", f.Request)
	if _, err := DecodeRenderFreezeResult(recovered.Body, recoveredID, f.Request); err != nil {
		t.Fatal(err)
	}
	changed := f.Request
	changed.ExpectedRevision = "999"
	_, err = service.Freeze(ctx, who, f.Tenant, f.Asset, "interrupted-freeze", changed)
	if err == nil || domain.AsError(err).Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("recovery lost request hash conflict", err)
	}
	service.Qualification = q
	store.failReads = false
	if _, err := admin.Exec(ctx, `CREATE FUNCTION photo.test_freeze_abort() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.render_registered_at IS NOT NULL THEN RAISE EXCEPTION 'controlled final registration failure';END IF;RETURN NEW;END $$; CREATE TRIGGER test_freeze_abort BEFORE UPDATE ON photo.v2_draft_uploads FOR EACH ROW EXECUTE FUNCTION photo.test_freeze_abort()`); err != nil {
		t.Fatal(err)
	}
	_, err = service.Freeze(ctx, who, f.Tenant, f.Asset, "interrupted-registration", f.Request)
	if err == nil {
		t.Fatal("controlled registration failure missing")
	}
	if _, err := admin.Exec(ctx, `DROP TRIGGER test_freeze_abort ON photo.v2_draft_uploads; DROP FUNCTION photo.test_freeze_abort()`); err != nil {
		t.Fatal(err)
	}
	reads, puts = store.reads, store.puts
	service.Qualification = nil
	store.failReads = true
	recovered, err = service.Freeze(ctx, who, f.Tenant, f.Asset, "interrupted-registration", f.Request)
	if err != nil || recovered.Status != 200 || store.reads != reads || store.puts != puts {
		t.Fatal("published ready recipe recovery failed", err)
	}
	service.Qualification = q
	store.failReads = false

	store.afterPut = func() {
		if _, err := admin.Exec(ctx, "UPDATE photo.v2_document_streams SET revision=revision+1 WHERE tenant_id=$1 AND id=$2", f.Tenant, f.Stream); err != nil {
			t.Fatal(err)
		}
	}
	_, err = service.Freeze(ctx, who, f.Tenant, f.Asset, "changed-during-publish", f.Request)
	if err == nil || domain.AsError(err).Code != "REVISION_CONFLICT" {
		t.Fatal("changed revision registered", err)
	}
	racedID, _ := RenderFreezeDocumentID(f.Tenant, f.Actor, f.Asset, "changed-during-publish", f.Request)
	var registered bool
	if err := admin.QueryRow(ctx, "SELECT render_registered_at IS NOT NULL FROM photo.v2_draft_uploads WHERE tenant_id=$1 AND document_id=$2", f.Tenant, racedID).Scan(&registered); err != nil || registered {
		t.Fatal("failed fence left registration", err)
	}
	var count int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key='changed-during-publish' AND response_status IS NOT NULL", f.Tenant, f.Actor, "Render.Freeze."+f.Asset.String()).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed fence stored reply", err)
	}
}

// Block both API sessions on the same actual PostgreSQL row before releasing
// them. Different model snapshots must converge on one immutable winner.
func assertCompetingFreezePins(t *testing.T, parent context.Context, admin *pgxpool.Pool, service SQLRenderFreezeService, who auth.Identity, tid, asset uuid.UUID, request RenderFreezeRequest, first []byte, frozen v2draft.FrozenRenderRecipe) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	frozen.Scalars.ModelLockSHA = strings.Repeat("f", 64)
	second, err := canonical.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2draft.DecodeFrozenRenderRecipe(second); err != nil || bytes.Equal(first, second) {
		t.Fatal("distinct compiled fixture", err)
	}
	scope := idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "Render.Freeze." + asset.String(), Key: "competing-pins"}
	id, _ := RenderFreezeDocumentID(tid, who.UserID, asset, scope.Key, request)
	hash, _ := canonical.Hash(request)
	if _, err := admin.Exec(ctx, `INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,$3,$4,$5,clock_timestamp()+interval '24 hours')`, tid, who.UserID, scope.Route, scope.Key, hash); err != nil {
		t.Fatal(err)
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(parent)
	var blocker int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid() FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key=$4 FOR UPDATE`, tid, who.UserID, scope.Route, scope.Key).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		body      []byte
		completed bool
		err       error
	}
	results := make(chan outcome, 2)
	for _, candidate := range [][]byte{first, second} {
		go func(body []byte) {
			pinned, completed, err := service.pinFreezePending(ctx, who, asset, scope, request, id, body)
			results <- outcome{pinned, completed, err}
		}(candidate)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := admin.QueryRow(ctx, `WITH RECURSIVE blocked(pid) AS (SELECT pid FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) UNION SELECT a.pid FROM pg_stat_activity a JOIN blocked b ON b.pid=ANY(pg_blocking_pids(a.pid))) SELECT count(*) FROM blocked`, blocker).Scan(&waiting); err != nil {
			t.Fatal("competing pins wait", err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case early := <-results:
			t.Fatal("competing pin returned before blocker release", early.err)
		case <-ctx.Done():
			t.Fatal("both competing pins did not wait on row")
		case <-ticker.C:
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.completed || b.completed || !bytes.Equal(a.body, b.body) || (!bytes.Equal(a.body, first) && !bytes.Equal(a.body, second)) {
		t.Fatal("competing snapshots did not converge", a.err, b.err)
	}
	var stored []byte
	if err := admin.QueryRow(ctx, `SELECT response_body FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route=$3 AND key=$4`, tid, who.UserID, scope.Route, scope.Key).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	pinned, err := decodeRenderFreezePending(stored, id, request)
	if err != nil || !bytes.Equal(pinned, a.body) {
		t.Fatal("stored winner differs", err)
	}
}
