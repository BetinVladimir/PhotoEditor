package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

// This fixture covers content compilation only; private verified markers do not
// stand in for the production physical reader or relational SQL authority.
type shownRetouchContentFixture struct {
	child uuid.UUID
	approved ApprovedTaskDraft
	selected LockedRetouchPreviewSelection
	physical PhysicalRetouchPreview
	candidate, intent []byte
	docs ExposureSourceDocuments
	owner ExposureOwnerCosts
	limits ExposureResourceEstimate
	pin RetouchQualificationPin
	alpha v2input.Ref
}

// Own content compilation fixture; it grants no physical or SQL authority.
func fullShownRetouchContentFixture(t *testing.T) shownRetouchContentFixture {
	t.Helper()
	child, approved, selected, physical := shownRetouchCompileFixture(t)
	encode := func(v any) []byte {
		b, e := canonical.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	ref := func(b []byte) v2input.Ref {
		s := sha256.Sum256(b)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(s[:])}
	}
	arbitrary := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	raw, e := os.ReadFile("../review/testdata/working-images-python.json")
	if e != nil {
		t.Fatal(e)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("images")
	}
	// Choose the real dense-warp fixture and add a separately pinned alpha plane.
	var sourceBody []byte
	for _, body := range images {
		if strings.Contains(string(body), "dense_warp") {
			sourceBody = body
			break
		}
	}
	if sourceBody == nil {
		t.Fatal("dense fixture missing")
	}
	var sourceMap map[string]any
	if json.Unmarshal(sourceBody, &sourceMap) != nil {
		t.Fatal("source map")
	}
	alpha := arbitrary()
	sourceMap["alpha"] = alpha
	sourceBody = encode(sourceMap)
	image, e := review.ParseWorkingImage(sourceBody)
	if e != nil {
		t.Fatal(e)
	}
	p := physical.linkage.Presentation.Params
	p.Source.Source = ref(sourceBody)
	p.Source.CanvasHash, e = canonical.Hash(image.Canvas)
	if e != nil {
		t.Fatal(e)
	}
	mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
	maskBody := encode(mask)
	maskRef := ref(maskBody)
	mask.Protected = true
	mask.Artifact.ID = uuid.New()
	protectedBody := encode(mask)
	p.Protection = ref(protectedBody)
	transform := arbitrary()
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "retouch_region", Reference: p.Source, ReferenceMask: arbitrary(), TargetSnapshotID: p.SnapshotID, Strength: .5, PreservePersons: true}
	intentBody := encode(intent)
	p.Intent = ref(intentBody)
	candidateBody := encode(map[string]any{"schema_version": 1, "target": p.Source, "mask": maskRef, "transform": transform, "confidence": .9, "state": "proposed", "reason": nil})
	p.Candidate = ref(candidateBody)
	physical.linkage.Presentation.Params = p
	for _, dest := range []*v2input.Ref{&physical.linkage.Presentation.Before, &physical.linkage.Presentation.After, &physical.linkage.Presentation.Affected, &physical.linkage.Presentation.Protected} {
		*dest = arbitrary()
	}
	// Populate the exact raw document bundle for the real preparation pipeline.
	sourceMap["pixels"] = physical.linkage.Presentation.Pixels
	physical.image.ImageBytes = encode(sourceMap)
	physical.linkage.Presentation.WorkingImage = ref(physical.image.ImageBytes)
	evidence := RetouchPreviewEvidence{SchemaVersion: 1, Params: p, WorkingImage: physical.linkage.Presentation.WorkingImage, Pixels: physical.linkage.Presentation.Pixels, Algorithm: "beautyfilter_v1", ProtectedPixelsUnchanged: true, OutsideMaskUnchanged: true, AlphaUnchanged: true, ROI: []int64{0, 0, int64(image.Canvas.Width), int64(image.Canvas.Height)}}
	physical.linkage.Evidence.raw = encode(evidence)
	physical.linkage.Presentation.NumericalEvidence = ref(physical.linkage.Evidence.raw)
	physical.linkage.Document.raw = encode(physical.linkage.Presentation)
	physical.linkage.Document.Source.Ref = ref(physical.linkage.Document.raw)
	selected.Receipt.WorkingImage = physical.linkage.Presentation.WorkingImage
	selected.Receipt.NumericalEvidence = physical.linkage.Presentation.NumericalEvidence
	selected.Receipt.Presentation = physical.linkage.Document.Source.Ref
	_, selected.Receipt.ParamsSHA, e = FreezeRetouchPreviewParams(p)
	if e != nil {
		t.Fatal(e)
	}
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: p.TargetID, Source: p.Source, Intent: p.Intent, Candidate: p.Candidate, Mask: maskRef, Transform: transform, Kind: "retouch_region", Strength: .5, PreservePersons: true}
	approved.Body = encode(edit)
	approved.Params = ref(approved.Body)
	docs := ExposureSourceDocuments{Source: sourceBody, Reference: sourceBody, Mask: maskBody, Protection: protectedBody}
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	pin := RetouchQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: strings.Repeat("a", 64), Backend: "cpu", ModelLockSHA: p.ModelLockSHA, RuntimeLockSHA: p.RuntimeLockSHA, DeviceProfileSHA: p.DeviceProfileSHA, DatasetSHA: strings.Repeat("a", 64), Report: arbitrary(), Calibration: arbitrary()}
	return shownRetouchContentFixture{child: child, approved: approved, selected: selected, physical: physical,
		candidate: candidateBody, intent: intentBody, docs: docs, owner: owner, limits: limits, pin: pin, alpha: alpha}
}

func TestShownRetouchFullInventoryAndQualifiedTask(t *testing.T) {
	f := fullShownRetouchContentFixture(t)
	child, approved, selected, physical := f.child, f.approved, f.selected, f.physical
	candidateBody, intentBody, docs := f.candidate, f.intent, f.docs
	owner, limits, pin, alpha := f.owner, f.limits, f.pin, f.alpha
	p := physical.linkage.Presentation.Params
	compiled, e := CompileShownRetouchAdoption(child, approved, selected, physical)
	if e != nil {
		t.Fatal(e)
	}
	closure, e := CompileShownRetouchAdoptionInventory(compiled, approved, physical, candidateBody, intentBody, docs)
	if e != nil || closure.Open == nil || closure.InputDigest == "" {
		t.Fatal("full closure", e)
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[uuid.UUID]int{}
	contracts := map[uuid.UUID]string{}
	for {
		entry, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		counts[entry.Ref.ArtifactID]++
		contracts[entry.Ref.ArtifactID] = entry.Contract
	}
	for id, count := range counts {
		if count != 1 {
			t.Fatal("duplicate dependency", id, count)
		}
	}
	if counts[alpha.ArtifactID] != 1 || contracts[alpha.ArtifactID] != "NpyArray" {
		t.Fatal("alpha missing")
	}
	geometry, err := v2input.ResolveSemanticGeometryArrays(p.Source, docs.Source)
	if err != nil || len(geometry) != 1 {
		t.Fatal("dense geometry", err)
	}
	for _, field := range geometry {
		if counts[field.Ref.ArtifactID] != 1 || contracts[field.Ref.ArtifactID] != "NpyArray" {
			t.Fatal("dense field missing")
		}
	}
	if counts[p.Source.Source.ArtifactID] != 1 {
		t.Fatal("shared source/reference not deduplicated")
	}
	for _, ref := range []v2input.Ref{physical.linkage.Presentation.Before, physical.linkage.Presentation.After, physical.linkage.Presentation.Affected, physical.linkage.Presentation.Protected} {
		if counts[ref.ArtifactID] != 1 || contracts[ref.ArtifactID] != "EncodedImage" {
			t.Fatal("shown media missing")
		}
	}
	task, e := CompileShownRetouchTask(compiled, approved, physical, candidateBody, intentBody, docs, owner, limits, pin)
	if e != nil {
		t.Fatal(e)
	}
	draft, e := task.DraftTask()
	if e != nil || draft.Operation != "semantic.adopt_shown_retouch_region" || draft.InputDigest != closure.InputDigest {
		t.Fatal("own task", e)
	}
	for _, mode := range []string{"backend", "model", "runtime", "device", "missing_report", "missing_source"} {
		changed := pin
		input := docs
		switch mode {
		case "backend":
			changed.Backend = "cuda"
		case "model":
			changed.ModelLockSHA = strings.Repeat("b", 64)
		case "runtime":
			changed.RuntimeLockSHA = strings.Repeat("b", 64)
		case "device":
			changed.DeviceProfileSHA = strings.Repeat("b", 64)
		case "missing_report":
			changed.Report = v2input.Ref{}
		case "missing_source":
			input.Source = nil
		}
		result, err := CompileShownRetouchTask(compiled, approved, physical, candidateBody, intentBody, input, owner, limits, changed)
		if err == nil || result.frozen.draft.ID != uuid.Nil {
			t.Fatal("invalid task accepted", mode, err)
		}
	}
	for _, mode := range []string{"valid", "before_factory", "after_factory", "after_publication", "canceled_factory"} {
		ctx, cancel := context.WithCancel(context.Background())
		recorder := &previewPublicationRecorder{}
		checks, factories := 0, 0
		recheck := func(context.Context) error {
			checks++
			if (mode == "before_factory" && checks == 1) || (mode == "after_factory" && checks == 2) || (mode == "after_publication" && checks == 3) {
				return ErrConflict
			}
			return nil
		}
		result, err := PrepareShownRetouchWithPublisher(ctx, child, approved, selected, physical, candidateBody, intentBody, docs, owner, limits, pin,
			func(FrozenShownRetouchTask) (v2input.Publisher, error) {
				factories++
				if mode == "canceled_factory" {
					cancel()
				}
				return recorder, nil
			}, recheck)
		cancel()
		if mode == "valid" {
			if err != nil || result.compiled.TaskID != compiled.TaskID || checks != 3 || len(result.documents) == 0 {
				t.Fatal("complete publication", err)
			}
		} else {
			if err == nil || result.compiled.TaskID != uuid.Nil || result.documents != nil {
				t.Fatal("failed preparation escaped", mode, err)
			}
			if mode != "after_publication" && len(recorder.calls) != 0 {
				t.Fatal("revoked preparation published", mode)
			}
			if mode == "before_factory" && factories != 0 {
				t.Fatal("revoked preparation constructed publisher")
			}
		}
	}

}
