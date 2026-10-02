package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/maintenance"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

type maskPixelsTestStore struct {
	t              *testing.T
	puts, recovers int
	uncertain      bool
	invalidReceipt bool
	object         objectstore.Object
}

func (s *maskPixelsTestStore) PutImmutable(_ context.Context, key string, reader io.Reader, size int64, sha, media string) (objectstore.Object, error) {
	s.puts++
	body, err := io.ReadAll(reader)
	if err != nil || int64(len(body)) != size || maskSHA(body) != sha || media != "application/x-npy" {
		s.t.Fatal("object bytes changed", err)
	}
	s.object = objectstore.Object{Key: key, Bytes: size, SHA256: sha, ContentType: media, VersionID: "synthetic-version"}
	if s.uncertain {
		return objectstore.Object{}, objectstore.ErrUnavailable
	}
	if s.invalidReceipt {
		bad := s.object
		bad.VersionID = "null"
		return bad, nil
	}
	return s.object, nil
}
func (s *maskPixelsTestStore) RecoverImmutable(_ context.Context, key string, size int64, sha, media string) (objectstore.Object, error) {
	s.recovers++
	if s.object.Key != key || s.object.Bytes != size || s.object.SHA256 != sha || s.object.ContentType != media {
		s.t.Fatal("recovery mismatch")
	}
	return s.object, nil
}

func TestIntegrationUserMaskPixelsPublisher(t *testing.T) {
	raw := os.Getenv("PHOTO_MASK_PIXELS_CASE")
	if raw == "" {
		t.Skip("isolated committed original-version fixture required")
	}
	var fixture struct {
		Tenant, Actor, Session uuid.UUID
		Params                 string
	}
	if err := json.Unmarshal([]byte(raw), &fixture); err != nil {
		t.Fatal(err)
	}
	var params LayerComposeParams
	if err := json.Unmarshal([]byte(fixture.Params), &params); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects := &maskPixelsTestStore{t: t}
	publisher := UserMaskPixelsPublisher{DB: pool, Objects: objects, Bucket: "test"}
	body := maskNPY("{'descr':'<f4','fortran_order':False,'shape':(2,3)}\n", []float32{0, .5, 1, 1, .25, 0})
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	publish := func(document uuid.UUID) (v2input.Ref, error) {
		return publisher.Publish(ctx, who, fixture.Tenant, params.Base.AssetVersionID, document, bytes.NewReader(body), int64(len(body)), maskSHA(body), 3, 2)
	}
	document := uuid.New()
	first, err := publish(document)
	if err != nil {
		t.Fatal("binary publication", err)
	}
	second, err := publish(document)
	if err != nil || second != first || objects.puts != 1 || objects.recovers != 0 {
		t.Fatal("binary replay", err)
	}
	badBody := append([]byte(nil), body...)
	badBody[len(badBody)-1] = 0xff
	if _, err := publisher.Publish(ctx, who, fixture.Tenant, params.Base.AssetVersionID, uuid.New(), bytes.NewReader(badBody), int64(len(badBody)), maskSHA(badBody), 3, 2); err == nil || objects.puts != 1 {
		t.Fatal("invalid probabilities reached object store", err)
	}
	objects.uncertain = true
	recovered, err := publish(uuid.New())
	if err != nil || recovered.ArtifactID == first.ArtifactID || objects.puts != 2 || objects.recovers != 1 {
		t.Fatal("uncertain PUT recovery", err)
	}
	objects.uncertain = false
	objects.invalidReceipt = true
	receiptDocument := uuid.New()
	if _, err := publish(receiptDocument); !errors.Is(err, v2input.ErrInvalid) || objects.puts != 3 {
		t.Fatal("invalid immutable receipt accepted", err)
	}
	objects.invalidReceipt = false
	if _, err := publish(receiptDocument); err != nil || objects.puts != 4 {
		t.Fatal("fenced resume after invalid receipt", err)
	}
}

// This case uses production immutable PUT/recovery against versioned MinIO.
func TestIntegrationUserMaskPixelsS3(t *testing.T) {
	raw := os.Getenv("PHOTO_MASK_PIXELS_S3_CASE")
	if raw == "" {
		t.Skip("isolated PostgreSQL and versioned MinIO required")
	}
	var fixture struct {
		Tenant, Project, Actor, Session uuid.UUID
		Params                          string
		Objects                         objectstore.Config
	}
	if err := json.Unmarshal([]byte(raw), &fixture); err != nil {
		t.Fatal(err)
	}
	var params LayerComposeParams
	if err := json.Unmarshal([]byte(fixture.Params), &params); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object store configuration invalid")
	}
	sources := v2draft.SQLArtifactCatalog{DB: pool, Bucket: "test", ProjectID: fixture.Project, AssetVersionID: params.Base.AssetVersionID}
	whoSource := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	if _, err := sources.Lookup(ctx, whoSource, fixture.Tenant, v2draft.LayerRef{Ref: params.Base.Source, Contract: "WorkingImage"}); err != nil {
		t.Fatal("exact source version", err)
	}
	sources.AssetVersionID = uuid.New()
	if _, err := sources.Lookup(ctx, whoSource, fixture.Tenant, v2draft.LayerRef{Ref: params.Base.Source, Contract: "WorkingImage"}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("foreign version read source", err)
	}
	publisher := UserMaskPixelsPublisher{DB: pool, Objects: objects, Bucket: fixture.Objects.Bucket}
	body := maskNPY("{'descr':'<f4','fortran_order':False,'shape':(2,3)}\n", []float32{0, .5, 1, 1, .25, 0})
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	document := uuid.New()
	publish := func() (v2input.Ref, error) {
		return publisher.Publish(ctx, who, fixture.Tenant, params.Base.AssetVersionID, document, bytes.NewReader(body), int64(len(body)), maskSHA(body), 3, 2)
	}
	first, err := publish()
	if err != nil {
		t.Fatal("S3 binary publication", err)
	}
	second, err := publish()
	if err != nil || first != second {
		t.Fatal("S3 publication replay", err)
	}
	catalog := SQLUserMaskPixelsCatalog{DB: pool, Bucket: fixture.Objects.Bucket}
	bound, err := catalog.LookupUserMaskPixels(ctx, who, fixture.Tenant, params.Base.AssetVersionID, first, 3, 2)
	if err != nil || bound.SHA256 != first.SHA256 {
		t.Fatal("user binary scope", err)
	}
	for _, dims := range [][2]int64{{2, 3}, {3, 3}, {0, 2}} {
		if _, err := catalog.LookupUserMaskPixels(ctx, who, fixture.Tenant, params.Base.AssetVersionID, first, dims[0], dims[1]); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("wrong canvas authorized", err)
		}
	}
	if _, err := catalog.LookupUserMaskPixels(ctx, who, fixture.Tenant, uuid.New(), first, 3, 2); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("foreign version authorized", err)
	}
	key, err := objectstore.V2DraftKey(fixture.Tenant, first.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := objects.RecoverImmutable(ctx, key, int64(len(body)), maskSHA(body), "application/x-npy")
	if err != nil {
		t.Fatal("S3 immutable recovery", err)
	}
	reader, err := objects.Open(ctx, receipt)
	if err != nil {
		t.Fatal(err)
	}
	verification := VerifyUserMaskNPY(ctx, reader, receipt.Bytes, receipt.SHA256, 3, 2)
	closeErr := reader.Close()
	if verification != nil || closeErr != nil {
		t.Fatal("pinned S3 probability validation", verification, closeErr)
	}
}

func TestIntegrationUserMaskPixelsRevokedScope(t *testing.T) {
	raw := os.Getenv("PHOTO_MASK_PIXELS_REVOKED_CASE")
	if raw == "" {
		t.Skip("committed revoked session and ready binary required")
	}
	var fixture struct {
		Tenant, Actor, Session, Version uuid.UUID
		Ref                             v2input.Ref
		Bucket                          string
	}
	if err := json.Unmarshal([]byte(raw), &fixture); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	catalog := SQLUserMaskPixelsCatalog{DB: pool, Bucket: fixture.Bucket}
	if _, err := catalog.LookupUserMaskPixels(ctx, auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}, fixture.Tenant, fixture.Version, fixture.Ref, 3, 2); err == nil {
		t.Fatal("revoked session authorized ready mask")
	}
}

func TestIntegrationVerifiedUserMaskS3(t *testing.T) {
	raw := os.Getenv("PHOTO_VERIFIED_MASK_S3_CASE")
	if raw == "" {
		t.Skip("real source document and versioned object store required")
	}
	var fixture struct {
		Tenant, Project, Snapshot, Actor, Session uuid.UUID
		Params, SourceBody                        string
		Objects                                   objectstore.Config
	}
	if err := json.Unmarshal([]byte(raw), &fixture); err != nil {
		t.Fatal(err)
	}
	var params LayerComposeParams
	if err := json.Unmarshal([]byte(fixture.Params), &params); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object store configuration invalid")
	}
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	values := make([]float32, 400*300)
	for i := range values {
		values[i] = float32(i%5) / 4
	}
	body := maskNPY("{'descr':'<f4','fortran_order':False,'shape':(300,400)}\n", values)
	binaryPublisher := UserMaskPixelsPublisher{DB: pool, Objects: objects, Bucket: fixture.Objects.Bucket}
	binary, err := binaryPublisher.Publish(ctx, who, fixture.Tenant, params.Base.AssetVersionID, uuid.New(), bytes.NewReader(body), int64(len(body)), maskSHA(body), 400, 300)
	if err != nil {
		t.Fatal("new array", err)
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixture.SourceBody), &source); err != nil {
		t.Fatal(err)
	}
	mask, err := canonical.Marshal(map[string]any{"artifact": binary, "canvas": source["canvas"], "transform_chain": source["transform_chain"], "object_id": nil, "selector": "user", "confidence": 1, "model_lock_id": nil, "protected": false})
	if err != nil {
		t.Fatal(err)
	}
	sources := v2draft.SQLArtifactCatalog{DB: pool, Bucket: fixture.Objects.Bucket, ProjectID: fixture.Project, AssetVersionID: params.Base.AssetVersionID}
	pixels := SQLUserMaskPixelsCatalog{DB: pool, Bucket: fixture.Objects.Bucket}
	publisher := v2draft.Publisher{DB: pool, Objects: objects, Bucket: fixture.Objects.Bucket}
	document := uuid.New()
	publish := func() (v2input.Ref, error) {
		return PublishVerifiedUserCompositionMask(ctx, who, fixture.Tenant, document, params.Base, mask, sources, pixels, objects, publisher)
	}
	first, err := publish()
	if err != nil {
		t.Fatal("verified user mask", err)
	}
	second, err := publish()
	if err != nil || first != second {
		t.Fatal("verified user mask replay", err)
	}
	params.Mask, err = canonical.Marshal(map[string]any{"op": "ref", "mask": first, "canvas_hash": params.Base.CanvasHash})
	if err != nil {
		t.Fatal(err)
	}
	paramsRaw, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	schedulerConfig := config.Copy()
	schedulerConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, schedulerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	preflight := SQLOriginalCompositionPreflight{DB: scheduler}
	if err := preflight.CheckUserMaskPixels(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, paramsRaw, first, binary, 400, 300); err != nil {
		t.Fatal("bound mask input", err)
	}
	if err := preflight.CheckUserMaskPixels(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, paramsRaw, first, binary, 300, 400); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("wrong input dimensions", err)
	}
	if err := preflight.CheckUserMaskSet(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, paramsRaw, 400, 300); err != nil {
		t.Fatal("complete user mask set", err)
	}
	foreignMask := v2input.Ref{ArtifactID: uuid.New(), SHA256: first.SHA256}
	params.ProtectedMask = &foreignMask
	wrongProtected, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.CheckUserMaskSet(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, wrongProtected, 400, 300); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("unverified protected mask accepted", err)
	}
	params.ProtectedMask = &first
	params.Mask, err = canonical.Marshal(map[string]any{"op": "invert", "operand": map[string]any{"op": "union", "left": json.RawMessage(params.Mask), "right": json.RawMessage(params.Mask)}})
	if err != nil {
		t.Fatal(err)
	}
	nestedParams, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.CheckUserMaskSet(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, nestedParams, 400, 300); err != nil {
		t.Fatal("nested shared protected mask", err)
	}
	params.ProtectedMask = nil
	run := uuid.New()
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(workflow, "layer.compose", 0)
	if err != nil {
		t.Fatal(err)
	}
	request := CompositionPreparation{Tenant: fixture.Tenant, Task: task, Actor: who, Base: params.Base, SourceResult: params.SourceResult, ProposalResult: params.ProposalResult, ProtectedMask: params.ProtectedMask, Width: 400, Height: 300, Layer: Layer{ID: uuid.New(), Enabled: true, Opacity: params.Opacity, Blend: params.Blend, Operation: "fixture.adjustment", OperationVersion: "1", Params: params.ProposalResult, Mask: params.Mask}}
	prepared, err := PublishCompositionParams(ctx, request, publisher)
	if err != nil {
		t.Fatal("user-masked task params", err)
	}
	compiled, err := CompileComposition(prepared.Layer, prepared.Base, prepared.SourceResult, prepared.ProposalResult, prepared.ProtectedMask, prepared.Width, prepared.Height)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCompositionMasks(ctx, who, fixture.Tenant, compiled, prepared.ParamsRef, sources, objects); err != nil {
		t.Fatal("full pinned user mask set", err)
	}
	roots, err := CompositionInputRoots(compiled, prepared.ParamsRef)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompileCompositionDraftConfig(ctx, fixture.Project, fixture.Snapshot, fixture.Session, run, []CompositionDraftRequest{{Ordinal: 0, Preparation: prepared, Resolved: roots}})
	if err != nil {
		t.Fatal(err)
	}
	drafts := SQLOriginalCompositionDraftStore{DB: &preflight}
	if err := drafts.Begin(ctx, fixture.Tenant, run, who, maskSHA([]byte("synthetic-policy")), plan); err != nil {
		t.Fatal("user-masked draft", err)
	}
	stage := SQLOriginalCompositionTaskStage{DB: scheduler}
	if err := stage.StageUserMasked(ctx, fixture.Tenant, run, workflow, 0, plan.Tasks[0]); err != nil {
		t.Fatal("atomic user-masked stage", err)
	}
	if err := stage.StageUserMasked(ctx, fixture.Tenant, run, workflow, 0, plan.Tasks[0]); err != nil {
		t.Fatal("atomic user-masked replay", err)
	}
	wrong := binary
	wrong.ArtifactID = uuid.New()
	if err := preflight.CheckUserMaskPixels(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, paramsRaw, first, wrong, 400, 300); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("foreign mask pixels", err)
	}
	object, err := sources.Lookup(ctx, who, fixture.Tenant, v2draft.LayerRef{Ref: first, Contract: "MaskRef"})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, mask) {
		t.Fatal("pinned typed document bytes", readErr, closeErr)
	}
}

func TestIntegrationUserMaskGC(t *testing.T) {
	raw := os.Getenv("PHOTO_MASK_GC_CASE")
	if raw == "" {
		t.Skip("expired isolated binary mask required")
	}
	var fixture struct{ Objects objectstore.Config }
	if err := json.Unmarshal([]byte(raw), &fixture); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object store configuration invalid")
	}
	cleaner := maintenance.ArtifactCleaner{Repository: maintenance.SQLArtifactGC{DB: pool}, Objects: objects, Bucket: fixture.Objects.Bucket}
	done, err := cleaner.CleanOne(ctx)
	if err != nil || !done {
		t.Fatal("binary GC", err)
	}
	done, err = cleaner.CleanOne(ctx)
	if err != nil || done {
		t.Fatal("GC repeated mutation", err)
	}
}
