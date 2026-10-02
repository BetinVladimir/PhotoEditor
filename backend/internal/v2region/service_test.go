package v2region

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type regionMetadataRow struct{ value RegionSourceMetadata }

func (r regionMetadataRow) Scan(d ...any) error {
	v := r.value
	*d[0].(*uuid.UUID) = v.ProjectID
	*d[1].(*uuid.UUID) = v.Ref.ArtifactID
	*d[2].(*string) = v.Ref.SHA256
	*d[3].(*string) = "private"
	*d[4].(*string) = v.Object.Key
	*d[5].(*string) = v.Object.VersionID
	*d[6].(*int64) = v.Object.Bytes
	*d[7].(*string) = v.Object.ContentType
	return nil
}

type regionPrepareDB struct {
	source, recipe RegionSourceMetadata
	calls          int
}

func (db *regionPrepareDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	db.calls++
	if strings.Contains(q, "v26_region_recipe") {
		return regionMetadataRow{db.recipe}
	}
	return regionMetadataRow{db.source}
}

type regionPrepareObjects struct {
	bodies  map[string][]byte
	streams []*regionTestStream
}

func (o *regionPrepareObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	stream := &regionTestStream{Reader: bytes.NewReader(o.bodies[obj.Key])}
	o.streams = append(o.streams, stream)
	return stream, nil
}
func TestPrepareRegionCombinesScopeAndExactBytes(t *testing.T) {
	req, recipeRaw, sourceRef, sourceRaw := regionEvidenceFixture(t)
	project := uuid.New()
	metadata := func(ref v2input.Ref, key string, raw []byte) RegionSourceMetadata {
		return RegionSourceMetadata{ProjectID: project, Ref: ref, Object: objectstore.Object{Key: key, VersionID: "pinned", Bytes: int64(len(raw)), ContentType: "application/json"}}
	}
	for _, foreign := range []bool{false, true} {
		db := &regionPrepareDB{source: metadata(sourceRef, "source", sourceRaw), recipe: metadata(v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}, "recipe", recipeRaw)}
		if foreign {
			db.recipe.ProjectID = uuid.New()
		}
		objects := &regionPrepareObjects{bodies: map[string][]byte{"source": sourceRaw, "recipe": recipeRaw}}
		out, err := (Service{db, objects, "private"}).Prepare(context.Background(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), req)
		if foreign {
			if err != ErrRegionInvalid || len(objects.streams) != 0 {
				t.Fatal("foreign project triggered object IO")
			}
			continue
		}
		if err != nil || out.ProjectID != project || out.Evidence.Geometry.OutputWidth != 1024 || db.calls != 2 || len(objects.streams) != 2 {
			t.Fatalf("preparation failed %v", err)
		}
		for _, stream := range objects.streams {
			if !stream.closed {
				t.Fatal("document stream leaked")
			}
		}
	}
}

func TestPreparedPlanPinsScopeVersionsAndGeometry(t *testing.T) {
	req, recipeRaw, sourceRef, sourceRaw := regionEvidenceFixture(t)
	project, tenant, asset := uuid.New(), uuid.New(), uuid.New()
	db := &regionPrepareDB{
		source: RegionSourceMetadata{ProjectID: project, Ref: sourceRef, Object: objectstore.Object{Key: "source", VersionID: "source-v1", Bytes: int64(len(sourceRaw)), ContentType: "application/json"}},
		recipe: RegionSourceMetadata{ProjectID: project, Ref: v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}, Object: objectstore.Object{Key: "recipe", VersionID: "recipe-v1", Bytes: int64(len(recipeRaw)), ContentType: "application/json"}},
	}
	objects := &regionPrepareObjects{bodies: map[string][]byte{"source": sourceRaw, "recipe": recipeRaw}}
	service := Service{db, objects, "private"}
	prepare := func(r RegionPreviewRequest, scope uuid.UUID) FrozenRegionPlan {
		t.Helper()
		out, err := service.Prepare(context.Background(), scope, asset, uuid.New(), uuid.New(), r)
		if err != nil {
			t.Fatal(err)
		}
		return out.Plan
	}
	base := prepare(req, tenant)
	requirements := base.InputRequirements()
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: base.SHA256()}
	task := uuid.New()
	publisher := &regionParamsFixture{digest: base.SHA256()}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	first, err := base.PublishParams(context.Background(), who, task, publisher)
	if err != nil || first.SHA256 != base.SHA256() || publisher.tenant != tenant || publisher.version != req.VersionID {
		t.Fatal("publication scope", err)
	}
	initialID := publisher.document
	if _, err := base.PublishParams(context.Background(), who, task, publisher); err != nil || publisher.document != initialID {
		t.Fatal("unstable publication ID")
	}
	publisher.digest = regionSHA
	if _, err := base.PublishParams(context.Background(), who, task, publisher); err == nil {
		t.Fatal("publisher substituted bytes")
	}

	frozen, err := base.FreezeInputs(task, params, requirements)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]v2input.Entry(nil), requirements...)
	requirements[0].Contract = "external mutation"
	for attempt := 0; attempt < 2; attempt++ {
		cursor, err := frozen.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		publisher := &regionTransportPublisher{}
		built, err := v2input.BuildInputClosure(context.Background(), task, cursor, publisher)
		if err != nil || built.Count != 5 || publisher.calls != 2 {
			t.Fatalf("transport: %v %v", built, err)
		}
	}
	requirements = original
	other, err := base.FreezeInputs(uuid.New(), params, requirements)
	if err != nil || other.InputDigest == frozen.InputDigest {
		t.Fatal("task not bound")
	}
	badParams := params
	badParams.SHA256 = regionSHA
	if _, err := base.FreezeInputs(task, badParams, requirements); err == nil {
		t.Fatal("wrong Params digest accepted")
	}
	if _, err := base.FreezeInputs(task, params, append(requirements, requirements[0])); err == nil {
		t.Fatal("duplicate input accepted")
	}
	if _, err := base.FreezeInputs(task, params, requirements[1:]); err == nil {
		t.Fatal("missing input accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := frozen.Open(ctx); err == nil {
		t.Fatal("cancelled cursor opened")
	}

	if len(requirements) != 4 {
		t.Fatal("missing recipe/style/source/pixels")
	}
	resolver := &regionInventoryResolver{}
	for _, entry := range requirements {
		if entry.Contract != "FrozenRecipe" {
			resolver.entries = append(resolver.entries, entry)
		}
	}
	extra := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: regionSHA}, Contract: "Artifact"}
	resolver.entries = append(resolver.entries, extra)
	resolved, err := base.ResolveInputs(context.Background(), resolver, auth.Identity{})
	if err != nil || len(resolved) != 5 || resolver.tenant != tenant {
		t.Fatalf("resolved inventory %v %v", resolved, err)
	}
	resolver.entries = resolver.entries[1:]
	if _, err := base.ResolveInputs(context.Background(), resolver, auth.Identity{}); err == nil {
		t.Fatal("missing required input accepted")
	}
	if _, err := (FrozenRegionPlan{}).ResolveInputs(context.Background(), resolver, auth.Identity{}); err == nil {
		t.Fatal("empty plan accepted")
	}
	requirements[0].Contract = "mutated"
	if base.InputRequirements()[0].Contract == "mutated" {
		t.Fatal("caller mutated frozen requirements")
	}
	if base.SHA256() == "" || !bytes.Contains(base.JSON(), []byte(`"render_order":"full_canvas_then_crop_then_downscale"`)) {
		t.Fatal("missing immutable render contract")
	}
	copyBody := base.JSON()
	copyBody[0] = '!'
	if base.JSON()[0] != '{' {
		t.Fatal("caller mutated frozen bytes")
	}
	if prepare(req, tenant).SHA256() != base.SHA256() {
		t.Fatal("actor/session or repeat read changed content digest")
	}
	if prepare(req, uuid.New()).SHA256() == base.SHA256() {
		t.Fatal("tenant absent from digest")
	}
	changed := req
	changed.MaxEdge = 512
	if prepare(changed, tenant).SHA256() == base.SHA256() {
		t.Fatal("output scale absent from digest")
	}
	changed = req
	changed.Rect.X0 += 0.01
	if prepare(changed, tenant).SHA256() == base.SHA256() {
		t.Fatal("ROI absent from digest")
	}
	db.source.Object.VersionID = "source-v2"
	if prepare(req, tenant).SHA256() == base.SHA256() {
		t.Fatal("source version absent from digest")
	}
	db.source.Object.VersionID = "source-v1"
	db.recipe.Object.VersionID = "recipe-v2"
	if prepare(req, tenant).SHA256() == base.SHA256() {
		t.Fatal("recipe version absent from digest")
	}
}

type regionInventoryResolver struct {
	entries []v2input.Entry
	tenant  uuid.UUID
}

func (r *regionInventoryResolver) ResolveRegionInputs(_ context.Context, _ auth.Identity, tenant uuid.UUID, _ v2input.SessionVersionBinding, _ v2input.Ref, _ v2draft.CorrectionVector) ([]v2input.Entry, error) {
	r.tenant = tenant
	return append([]v2input.Entry(nil), r.entries...), nil
}

type regionTransportPublisher struct{ calls int }

func (p *regionTransportPublisher) Publish(_ context.Context, _ uuid.UUID, _ int, _ string, body []byte) (v2input.Ref, error) {
	p.calls++
	digest, err := canonical.Bytes(body)
	if err != nil {
		return v2input.Ref{}, err
	}
	hash, err := canonical.Hash(json.RawMessage(digest))
	if err != nil {
		return v2input.Ref{}, err
	}
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: hash}, nil
}

type regionParamsFixture struct {
	digest                    string
	tenant, version, document uuid.UUID
}

func (p *regionParamsFixture) Publish(_ context.Context, _ auth.Identity, tenant, version, document uuid.UUID, _ []byte) (v2input.Ref, error) {
	p.tenant, p.version, p.document = tenant, version, document
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: p.digest}, nil
}
