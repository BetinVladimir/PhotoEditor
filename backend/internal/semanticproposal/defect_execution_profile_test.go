package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestDefectExecutionProfileQualifiedCostsAndScope(t *testing.T) {
	m := manualPinTestProof()
	reader := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	expected := v2qualification.Expected{Operation: "defects.propose", Version: "1", Backend: "cpu"}
	body, _ := canonical.Marshal(expected)
	q := personEvidenceObject{contract: "QualificationReport", ref: m.objects.Root.ref, object: m.objects.Root.object, backend: "cpu"}
	proof := defectQualificationPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", expected: string(body), report: q, metrics: v2qualification.Metrics{PeakRSSBytes: 401, ScratchBytes: 503}}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: q.ref.SHA256}
	input := m.binding.Source
	published := PublishedDefectParams{Ref: params, Inputs: []review.ArtifactRef{{ID: input.ArtifactID, SHA256: input.SHA256}}}
	sizes := []v2input.SemanticInputSize{{Ref: params, Bytes: 512}, {Ref: input, Bytes: 768}}
	costs := v2input.SemanticQualifiedCosts{RAMBytes: 401, ScratchBytes: 503}
	compile := func(ctx context.Context, p defectQualificationPreparationProof, c v2input.SemanticQualifiedCosts) ([]byte, error) {
		return reader.compileDefectExecutionProfile(ctx, who, m.tenant, p, expected, 8, 8, published, sizes, c, 1<<40, 0, 1<<40, 1<<40)
	}
	if raw, err := compile(context.Background(), proof, costs); err != nil || len(raw) == 0 {
		t.Fatal("qualified profile", err)
	}
	bad := proof
	bad.session = uuid.New()
	if raw, err := compile(context.Background(), bad, costs); err != v2input.ErrInvalid || raw != nil {
		t.Fatal("foreign proof", err)
	}
	nodes := []v2draft.LayerRef{{Ref: params, Contract: "Params"}, {Ref: input, Contract: "WorkingImage"}}
	catalog := &defectSizeCatalog{}
	profile, err := reader.prepareCatalogDefectExecutionProfile(context.Background(), catalog, who, m.tenant, proof, expected, 8, 8, published, nodes, costs, 1<<40, 0, 1<<40, 1<<40)
	if err != v2qualification.ErrUnqualified || profile != nil || catalog.calls != 4 {
		t.Fatal("catalog without live qualification accepted", err, catalog.calls)
	}
	catalog = &defectSizeCatalog{}
	if raw, err := reader.prepareCatalogDefectExecutionProfile(context.Background(), catalog, who, m.tenant, proof, expected, 8, 8, published, []v2draft.LayerRef{nodes[0], nodes[0]}, costs, 1<<40, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || raw != nil || catalog.calls != 0 {
		t.Fatal("ambiguous descriptors reached catalog", err)
	}
	for _, roots := range [][]review.ArtifactRef{
		{{ID: uuid.New(), SHA256: input.SHA256}},
		{{ID: input.ArtifactID, SHA256: "invalid"}},
		{{ID: params.ArtifactID, SHA256: params.SHA256}},
		{{ID: input.ArtifactID, SHA256: input.SHA256}, {ID: input.ArtifactID, SHA256: input.SHA256}},
	} {
		badPublished := published
		badPublished.ManualRoots = roots
		if raw, err := reader.compileDefectExecutionProfile(context.Background(), who, m.tenant, proof, expected, 8, 8, badPublished, sizes, costs, 1<<40, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || raw != nil {
			t.Fatal("pure compiler accepted untracked manual roots", err)
		}
	}
	costs.RAMBytes = 400
	if raw, err := compile(context.Background(), proof, costs); err != v2input.ErrInvalid || raw != nil {
		t.Fatal("understated costs", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if raw, err := compile(ctx, proof, costs); !errors.Is(err, context.Canceled) || raw != nil {
		t.Fatal("cancelled profile", err)
	}
}

type defectSizeCatalog struct{ calls int }

func (c *defectSizeCatalog) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	c.calls++
	return objectstore.Object{Key: node.Ref.ArtifactID.String(), VersionID: "v1", SHA256: node.Ref.SHA256, Bytes: 512}, nil
}

func TestSourceDefectProfileDescriptorPreflightWithoutIO(t *testing.T) {
	m := manualPinTestProof()
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: m.binding.Source.SHA256}
	input := review.ArtifactRef{ID: m.binding.Source.ArtifactID, SHA256: m.binding.Source.SHA256}
	published := PublishedDefectParams{Ref: params, Inputs: []review.ArtifactRef{input}}
	nodes := []v2draft.LayerRef{{Ref: params, Contract: "Params"}, {Ref: m.binding.Source, Contract: "WorkingImage"}}
	if _, err := validateDefectProfileDescriptors(published, nodes); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]v2draft.LayerRef{nodes[:1], {nodes[0], nodes[0]}, {nodes[1], nodes[1]}, {nodes[0], {Ref: m.binding.Source, Contract: ""}}} {
		catalog := &defectSizeCatalog{}
		reader := SQLTransferSourceReader{}
		raw, err := reader.prepareSourceDefectExecutionProfile(context.Background(), catalog, auth.Identity{}, m.tenant, m.binding, TransferColorProfile{}, v2input.Ref{}, v2qualification.Expected{}, published, bad, v2input.SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
		if err != v2input.ErrInvalid || raw != nil || catalog.calls != 0 {
			t.Fatal("invalid descriptors reached IO", err, catalog.calls)
		}
	}
	duplicate := published
	duplicate.Inputs = []review.ArtifactRef{input, input}
	if _, err := validateDefectProfileDescriptors(duplicate, append(nodes, nodes[1])); err != v2input.ErrInvalid {
		t.Fatal("duplicate input accepted", err)
	}
}

func TestCatalogDefectProfileDescriptorPreflightWithoutIO(t *testing.T) {
	m := manualPinTestProof()
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: m.binding.Source.SHA256}
	published := PublishedDefectParams{Ref: params, Inputs: []review.ArtifactRef{{ID: m.binding.Source.ArtifactID, SHA256: m.binding.Source.SHA256}}}
	for _, nodes := range [][]v2draft.LayerRef{
		{{Ref: params, Contract: "WorkingImage"}, {Ref: m.binding.Source, Contract: "WorkingImage"}},
		{{Ref: params, Contract: "Params"}, {Ref: m.binding.Source}},
		{{Ref: params, Contract: "Params"}, {Ref: v2input.Ref{ArtifactID: m.binding.Source.ArtifactID, SHA256: "invalid"}, Contract: "WorkingImage"}},
	} {
		catalog := &defectSizeCatalog{}
		raw, err := (SQLTransferSourceReader{}).prepareCatalogDefectExecutionProfile(context.Background(), catalog, auth.Identity{}, m.tenant, defectQualificationPreparationProof{}, v2qualification.Expected{}, 8, 8, published, nodes, v2input.SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
		if err != v2input.ErrInvalid || raw != nil || catalog.calls != 0 {
			t.Fatal("invalid catalog descriptors reached IO", err, catalog.calls)
		}
	}
}

func TestDefectProfileManualRootsRemainDeclared(t *testing.T) {
	m := manualPinTestProof()
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: m.binding.Source.SHA256}
	root := review.ArtifactRef{ID: uuid.New(), SHA256: params.SHA256}
	published := PublishedDefectParams{Ref: params, Inputs: []review.ArtifactRef{root}, ManualRoots: []review.ArtifactRef{root}}
	nodes := []v2draft.LayerRef{{Ref: params, Contract: "Params"}, {Ref: v2input.Ref{ArtifactID: root.ID, SHA256: root.SHA256}, Contract: "MaskRef"}}
	if _, err := validateDefectProfileDescriptors(published, nodes); err != nil {
		t.Fatal("declared manual root", err)
	}
	for _, roots := range [][]review.ArtifactRef{
		{{ID: uuid.New(), SHA256: root.SHA256}},
		{{ID: root.ID, SHA256: "invalid"}},
		{root, root},
		{{ID: params.ArtifactID, SHA256: params.SHA256}},
	} {
		bad := published
		bad.ManualRoots = roots
		catalog := &defectSizeCatalog{}
		raw, err := (SQLTransferSourceReader{}).prepareCatalogDefectExecutionProfile(context.Background(), catalog, auth.Identity{}, m.tenant, defectQualificationPreparationProof{}, v2qualification.Expected{}, 8, 8, bad, nodes, v2input.SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
		if err != v2input.ErrInvalid || raw != nil || catalog.calls != 0 {
			t.Fatal("untracked manual root reached catalog", err, catalog.calls)
		}
	}
}

func TestDefectLandmarkDocumentReuseRequiresExactMetadata(t *testing.T) {
	body := []byte(`{"landmarks":[]}`)
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	object := objectstore.Object{Key: "landmarks", VersionID: "v1", SHA256: ref.SHA256, Bytes: int64(len(body)), ContentType: "application/json"}
	store := &transferReadStore{bodies: map[string][]byte{object.Key: body}}
	reader := SQLTransferSourceReader{Objects: store}
	documents := map[uuid.UUID]verifiedLandmarkDocument{}
	for i := 0; i < 2; i++ {
		got, err := reader.readDefectLandmarkDocument(context.Background(), documents, ref, object)
		if err != nil || string(got) != string(body) {
			t.Fatal("landmark read", err)
		}
	}
	borrowed, err := reader.readDefectLandmarkDocument(context.Background(), documents, ref, object)
	if err != nil {
		t.Fatal(err)
	}
	borrowed[0] = '!'
	owned, err := reader.readDefectLandmarkDocument(context.Background(), documents, ref, object)
	if err != nil || string(owned) != string(body) {
		t.Fatal("caller mutated cached original bytes", err)
	}
	if len(store.opened) != 1 || store.closed != 1 {
		t.Fatal("shared document reopened", len(store.opened), store.closed)
	}
	changed := object
	changed.VersionID = "v2"
	if raw, err := reader.readDefectLandmarkDocument(context.Background(), documents, ref, changed); err != v2input.ErrConflict || raw != nil {
		t.Fatal("changed metadata reused", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if raw, err := reader.readDefectLandmarkDocument(ctx, documents, ref, object); !errors.Is(err, context.Canceled) || raw != nil {
		t.Fatal("cancelled cache reused", err)
	}
}

func TestCorruptDefectLandmarksNeverEnterCache(t *testing.T) {
	original := []byte(`{"landmarks":[]}`)
	sum := sha256.Sum256(original)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	object := objectstore.Object{Key: "landmarks", VersionID: "v1", SHA256: ref.SHA256, Bytes: int64(len(original)), ContentType: "application/json"}
	corrupt := append([]byte(nil), original...)
	corrupt[0] = '!'
	store := &transferReadStore{bodies: map[string][]byte{object.Key: corrupt}}
	reader := SQLTransferSourceReader{Objects: store}
	documents := map[uuid.UUID]verifiedLandmarkDocument{}
	if raw, err := reader.readDefectLandmarkDocument(context.Background(), documents, ref, object); err == nil || raw != nil || len(documents) != 0 {
		t.Fatal("corrupt landmark cached", err)
	}
	store.bodies[object.Key] = original
	if raw, err := reader.readDefectLandmarkDocument(context.Background(), documents, ref, object); err != nil || string(raw) != string(original) || len(store.opened) != 2 || store.closed != 2 {
		t.Fatal("original recovery", err)
	}
}

func TestDefectProfileQualificationFamilyPreflight(t *testing.T) {
	defect := v2qualification.Expected{Operation: "defects.propose", Version: "1", Backend: "cpu"}
	faces := [2]v2qualification.Expected{{Operation: "face.landmarks", Version: "1"}, {Operation: "face.detect", Version: "1"}}
	masks := [2]v2qualification.Expected{{Operation: "face.parse", Version: "1"}, {Operation: "face.detect", Version: "1"}}
	if err := validateDefectProfileQualificationFamilies(defect, faces, &masks); err != nil {
		t.Fatal(err)
	}
	if err := validateDefectProfileQualificationFamilies(defect, faces, nil); err != nil {
		t.Fatal(err)
	}
	badDefect := defect
	badDefect.Backend = "gpu"
	if err := validateDefectProfileQualificationFamilies(badDefect, faces, &masks); err != v2qualification.ErrUnqualified {
		t.Fatal("unsupported defect backend", err)
	}
	for i := range faces {
		bad := faces
		bad[i].Version = "2"
		if err := validateDefectProfileQualificationFamilies(defect, bad, &masks); err != v2qualification.ErrUnqualified {
			t.Fatal("wrong face version", err)
		}
	}
	for i := range masks {
		bad := masks
		bad[i].Operation = "face.landmarks"
		if err := validateDefectProfileQualificationFamilies(defect, faces, &bad); err != v2qualification.ErrUnqualified {
			t.Fatal("wrong mask family", err)
		}
	}
}

func TestDefectSourceExecutionEvidenceFailureReturnsZero(t *testing.T) {
	reader := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{context.Background(), ctx} {
		got, err := reader.prepareSourceDefectExecutionEvidence(current, nil, auth.Identity{}, uuid.Nil, v2input.SessionVersionBinding{}, TransferColorProfile{}, v2input.Ref{}, v2qualification.Expected{}, PublishedDefectParams{}, nil, v2input.SemanticQualifiedCosts{}, 0, 0, 0, 0)
		if err == nil || got != (defectSourceExecutionPreparation{}) {
			t.Fatal("failed preparation retained evidence", err)
		}
	}
}

func TestDefectFaceExecutionEvidenceFailureReturnsZero(t *testing.T) {
	reader := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{context.Background(), ctx} {
		got, err := reader.prepareFaceQualifiedDefectExecutionEvidence(current, auth.Identity{}, uuid.Nil, v2input.SessionVersionBinding{}, TransferColorProfile{}, DefectOriginPins{}, [2]v2input.Ref{}, [2]v2qualification.Expected{}, v2input.Ref{}, v2qualification.Expected{}, PublishedDefectParams{}, nil, v2input.SemanticQualifiedCosts{}, 0, 0, 0, 0)
		if err == nil || got != (defectFaceExecutionPreparation{}) {
			t.Fatal("failed face preparation retained evidence", err)
		}
	}
}

func TestDefectMaskExecutionEvidenceFailureReturnsZero(t *testing.T) {
	reader := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{context.Background(), ctx} {
		got, err := reader.prepareMaskQualifiedDefectExecutionEvidence(current, auth.Identity{}, uuid.Nil, v2input.SessionVersionBinding{}, TransferColorProfile{}, DefectOriginPins{}, [2]v2input.Ref{}, [2]v2input.Ref{}, [2]v2qualification.Expected{}, [2]v2qualification.Expected{}, v2input.Ref{}, v2qualification.Expected{}, PublishedDefectParams{}, nil, v2input.SemanticQualifiedCosts{}, 0, 0, 0, 0)
		if err == nil || got != (defectMaskExecutionPreparation{}) {
			t.Fatal("failed mask preparation retained evidence", err)
		}
	}
}
