package v2draft

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
)

type donorCompositionCatalog struct {
	fixture closureFixture
	donor   v2input.Ref
	deny    bool
	mutate  func()
	calls   int
}

func (c *donorCompositionCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (objectstore.Object, error) {
	c.calls++
	if c.mutate != nil {
		f := c.mutate
		c.mutate = nil
		f()
	}
	if c.deny && node.Ref == c.donor {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return c.fixture.Lookup(ctx, who, tenant, node)
}

type donorClosureTestPublisher struct{}

func (donorClosureTestPublisher) Publish(_ context.Context, _ uuid.UUID, _ int, _ string, body []byte) (v2input.Ref, error) {
	sha, err := canonical.Hash(json.RawMessage(body))
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, err
}

type donorClosureFaultPublisher struct {
	mode  string
	input uuid.UUID
	id    uuid.UUID
	calls int
}

func (p *donorClosureFaultPublisher) Publish(_ context.Context, _ uuid.UUID, _ int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	if p.mode == "error" {
		return v2input.Ref{}, errors.New("transport failed")
	}
	sha, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		return v2input.Ref{}, err
	}
	id := uuid.New()
	if p.mode == "chunk_alias" && contract == "InputClosureChunk" || p.mode == "manifest_alias" && contract == "InputClosureManifest" {
		id = p.input
	}
	if p.mode == "repeat_id" {
		if p.id == uuid.Nil {
			p.id = id
		}
		id = p.id
	}
	if p.mode == "wrong_hash" {
		sha = strings.Repeat("0", 64)
	}
	return v2input.Ref{ArtifactID: id, SHA256: sha}, nil
}

func checkPreparedDonorInputs(t *testing.T, original closureFixture, loaded LoadedDonorTaskPlan, p v2portrait.OpenEyesParams, manual DonorManualProducer) {
	t.Helper()
	fixture := closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	for id, obj := range original.objects {
		fixture.objects[id] = obj
	}
	for key, body := range original.bodies {
		fixture.bodies[key] = append([]byte(nil), body...)
	}
	add := func(ref v2input.Ref, raw []byte) {
		key := ref.ArtifactID.String()
		fixture.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json", VersionID: "pinned"}
		fixture.bodies[key] = append([]byte(nil), raw...)
	}
	donor := p.Donor
	face := p.DonorFaceID
	observation := uuid.New()
	proof := v2portrait.DonorConfirmation{SchemaVersion: 1, PersonID: uuid.New(), Source: p.Base, Donor: &donor, SourceFaceID: p.SourceFaceID, DonorFaceID: &face, SourceObservationID: uuid.New(), DonorObservationID: &observation, ConsentID: uuid.New(), Epoch: "1", ActorID: uuid.New()}
	proofRaw, sha, err := proof.Compile()
	if err != nil {
		t.Fatal(err)
	}
	p.Confirmation.SHA256 = sha
	add(p.Confirmation, proofRaw)
	for i, binding := range []v2input.SessionVersionBinding{p.Base, p.Donor} {
		obs, id, ref := proof.SourceObservationID, p.SourceFaceID, p.SourceFaces
		if i == 1 {
			obs = *proof.DonorObservationID
			id = p.DonorFaceID
			ref = p.DonorFaces
		}
		points := make([]map[string]float64, 68)
		for j := range points {
			points[j] = map[string]float64{"x": .5, "y": .5}
		}
		raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "binding": binding, "faces": []any{map[string]any{"observation_id": obs, "face_id": id, "person_id": nil, "box": map[string]float64{"x0": 0, "y0": 0, "x1": 1, "y1": 1}, "landmarks": points, "embedding": nil, "quality": .9}}})
		if err != nil {
			t.Fatal(err)
		}
		ref.SHA256, _ = canonical.Hash(json.RawMessage(raw))
		add(ref, raw)
		if i == 0 {
			p.SourceFaces = ref
		} else {
			p.DonorFaces = ref
		}
	}
	add(p.Donor.Source, loaded.Images.DonorRaw)
	paramsRaw, _, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	loaded.Plan, err = v2portrait.CompileDonorTaskPlan(context.Background(), p.Base, p.Donor, loaded.Images.SourceRaw, loaded.Images.DonorRaw, paramsRaw, loaded.patchWidth, loaded.patchHeight, loaded.runtimeSHA, loaded.capacity)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "mutate", "denied", "calibration"} {
		catalog := &donorCompositionCatalog{fixture: fixture, donor: p.Donor.Source, deny: mode == "denied"}
		planFirst, producerFirst, imageFirst := loaded.Plan.Params[0], manual.ParamsRaw[0], loaded.Images.SourceRaw[0]
		if mode == "mutate" {
			catalog.mutate = func() { loaded.Plan.Params[0] ^= 1; manual.ParamsRaw[0] ^= 1; loaded.Images.SourceRaw[0] ^= 1 }
		}
		minEdge := .0001
		if mode == "calibration" {
			minEdge = math.NaN()
		}
		resolver := ClosureResolver{Catalog: catalog, Objects: fixture}
		got, err := resolver.PrepareLoadedDonorInputEvidence(context.Background(), auth.Identity{}, uuid.New(), loaded, catalog, manual, minEdge)
		loaded.Plan.Params[0] = planFirst
		manual.ParamsRaw[0] = producerFirst
		loaded.Images.SourceRaw[0] = imageFirst
		if mode == "calibration" && catalog.calls != 0 {
			t.Fatal("invalid calibration performed IO")
		}
		if mode == "denied" || mode == "calibration" {
			if err == nil || got.Entries != nil || got.Faces.ConfirmationRaw != nil || got.Masks.Entries != nil || got.Images != nil {
				t.Fatal("partial composed evidence", err)
			}
			continue
		}
		if err != nil || len(got.Entries) != len(got.Masks.Entries)+4 || len(got.Images) != len(loaded.Plan.PhysicalImageOccurrences) || len(got.Faces.SourceFacesRaw) == 0 {
			t.Fatal("composed inventory", mode, err, len(got.Entries), len(got.Masks.Entries))
		}
		origin, originErr := CompileDonorManualAdmissionBinding(context.Background(), loaded, got)
		if originErr != nil || origin.root != manual.RootRef || origin.original != manual.ParamsRef {
			t.Fatal("manual admission binding", mode, originErr)
		}
		checkDonorManualLock(t, loaded, got, origin)
		missingOriginal := got
		missingOriginal.Entries = append([]v2input.Entry(nil), got.Entries...)
		for i, entry := range missingOriginal.Entries {
			if entry.Ref == manual.ParamsRef {
				missingOriginal.Entries = append(missingOriginal.Entries[:i], missingOriginal.Entries[i+1:]...)
				break
			}
		}
		if _, err := CompileDonorManualAdmissionBinding(context.Background(), loaded, missingOriginal); err == nil {
			t.Fatal("missing original Params admitted")
		}
		paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: loaded.Plan.ParamsSHA}
		bound, err := BindDonorParamsInventory(context.Background(), loaded, got, paramsRef)
		if err != nil || len(bound.Entries) != len(got.Entries)+1 || len(bound.SHA256) != 64 {
			t.Fatal("bound Params inventory", err)
		}
		// The exact closure keeps donor task Params and the independent original
		// rasterizer Params. Dropping either breaks mask producer provenance.
		paramsCount, taskParamsCount, producerParamsCount := 0, 0, 0
		for _, entry := range bound.Entries {
			if entry.Contract == "Params" {
				paramsCount++
				if entry.Ref == paramsRef {
					taskParamsCount++
				}
				if entry.Ref == manual.ParamsRef {
					producerParamsCount++
				}
			}
		}
		if paramsCount != 2 || taskParamsCount != 1 || producerParamsCount != 1 {
			t.Fatal("task/original Params closure mismatch", paramsCount, taskParamsCount, producerParamsCount)
		}
		// A valid document cannot authorize a different geometry threshold.
		pin := donorCalibrationFixture(t)
		withCalibration := got
		withCalibration.calibration = &pin
		withCalibration.Entries, err = uniqueTaskInputs(append(append([]v2input.Entry(nil), got.Entries...), v2input.Entry{Ref: pin.ref, Contract: "Calibration"}))
		if err != nil {
			t.Fatal(err)
		}
		if result, err := BindDonorParamsInventory(context.Background(), loaded, withCalibration, paramsRef); err == nil || result.Entries != nil {
			t.Fatal("bound uncalibrated face evidence")
		}
		workflow := uuid.New()
		published := v2portrait.PublishedDonorTaskPlan{ParamsRef: paramsRef, Plan: loaded.Plan}
		frozen, err := FreezeDonorTask(context.Background(), workflow, 0, loaded, got, published)
		if err != nil {
			t.Fatal("freeze donor", err)
		}
		repeated, err := FreezeDonorTask(context.Background(), workflow, 0, loaded, got, published)
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := frozen.Descriptor()
		if err != nil {
			t.Fatal(err)
		}
		restored, err := RestoreDonorTaskDescriptor(context.Background(), descriptor, workflow, 0, loaded, got, published)
		if err != nil {
			t.Fatal("donor descriptor recovery", err)
		}
		restoredRaw, err := restored.Descriptor()
		if err != nil || string(restoredRaw) != string(descriptor) {
			t.Fatal("changed recovered descriptor", err)
		}
		for _, failure := range []string{"whitespace", "unknown", "schema", "reservation", "params", "task", "entries", "workflow", "cancelled"} {
			body := append([]byte(nil), descriptor...)
			wf, ctx := workflow, context.Background()
			switch failure {
			case "whitespace":
				body = append([]byte(" "), body...)
			case "workflow":
				wf = uuid.New()
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			default:
				var document map[string]any
				if json.Unmarshal(body, &document) != nil {
					t.Fatal("descriptor fixture")
				}
				switch failure {
				case "unknown":
					document["extra"] = true
				case "schema":
					document["schema_version"] = 2
				case "reservation":
					document["reserved_bytes"] = 1
				case "params":
					document["params"] = map[string]any{}
				case "task":
					document["task"].(map[string]any)["input_digest"] = strings.Repeat("0", 64)
				case "entries":
					document["entries"] = []any{}
				}
				body, err = canonical.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
			}
			if result, err := RestoreDonorTaskDescriptor(ctx, body, wf, 0, loaded, got, published); err == nil || result.entries != nil {
				t.Fatal("invalid recovered donor task", failure, err)
			}
		}
		draft, _ := frozen.DraftTask()
		retry, _ := repeated.DraftTask()
		if draft.ID != retry.ID || draft.InputDigest != retry.InputDigest || draft.ParamsArtifactID != paramsRef.ArtifactID {
			t.Fatal("nondeterministic donor task")
		}
		draft.ExecutionProfile[0] ^= 1
		unchanged, _ := frozen.DraftTask()
		if unchanged.ExecutionProfile[0] != loaded.Plan.Profile[0] {
			t.Fatal("mutable frozen profile")
		}
		graph, err := frozen.PublishInputClosure(context.Background(), donorClosureTestPublisher{})
		if err != nil || graph.ID != retry.ID || len(graph.Direct) != len(bound.Entries) || graph.Built.Count != len(bound.Entries) {
			t.Fatal("donor closure transport", err)
		}
		for _, failure := range []string{"chunk_alias", "manifest_alias", "repeat_id", "wrong_hash", "error", "cancelled"} {
			publisher := &donorClosureFaultPublisher{mode: failure, input: bound.Entries[0].Ref.ArtifactID}
			ctx := context.Background()
			if failure == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			graph, err := frozen.PublishInputClosure(ctx, publisher)
			if err == nil || graph.ID != uuid.Nil || graph.Open != nil || graph.Direct != nil {
				t.Fatal("invalid donor transport escaped", failure, err)
			}
			if failure == "cancelled" && publisher.calls != 0 {
				t.Fatal("cancelled transport performed IO")
			}
		}
		priorEntry := got.Entries[0]
		got.Entries[0].Ref.ArtifactID = uuid.New()
		frozenGraph, err := frozen.PublishInputClosure(context.Background(), donorClosureTestPublisher{})
		got.Entries[0] = priorEntry
		if err != nil || frozenGraph.Direct[0] != bound.Entries[0].Ref {
			t.Fatal("caller inventory changed frozen inputs", err)
		}
		for _, failure := range []string{"workflow", "ordinal", "profile"} {
			wf, ordinal, pub := workflow, 0, published
			switch failure {
			case "workflow":
				wf = uuid.Nil
			case "ordinal":
				ordinal = -1
			case "profile":
				pub.Plan.Profile = append([]byte(nil), published.Plan.Profile...)
				pub.Plan.Profile[0] ^= 1
			}
			invalid, err := FreezeDonorTask(context.Background(), wf, ordinal, loaded, got, pub)
			if err == nil || invalid.draft.ID != uuid.Nil || invalid.entries != nil {
				t.Fatal("invalid freeze accepted", failure, err)
			}
		}
		for _, alteration := range []string{"alias", "hash", "missing_entry", "geometry"} {
			changed := got
			ref := paramsRef
			switch alteration {
			case "alias":
				ref.ArtifactID = manual.ParamsRef.ArtifactID
			case "hash":
				ref.SHA256 = p.Confirmation.SHA256
			case "missing_entry":
				changed.Entries = append([]v2input.Entry(nil), got.Entries[:len(got.Entries)-1]...)
			case "geometry":
				changed.Faces.Geometry.SourceLandmarks[0][0]++
			}
			if result, err := BindDonorParamsInventory(context.Background(), loaded, changed, ref); err == nil || result.Entries != nil || result.SHA256 != "" {
				t.Fatal("invalid bound inventory", alteration, err)
			}
		}
		for i := 1; i < len(got.Entries); i++ {
			if got.Entries[i-1].Ref.ArtifactID.String() >= got.Entries[i].Ref.ArtifactID.String() {
				t.Fatal("unsorted inventory")
			}
		}
	}
}
