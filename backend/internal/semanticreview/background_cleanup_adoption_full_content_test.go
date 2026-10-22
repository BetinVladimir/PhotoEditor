package semanticreview

import (
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
type shownCleanupContentFixture struct {
	child uuid.UUID
	approved ApprovedTaskDraft
	selected LockedBackgroundCleanupPreviewSelection
	physical PhysicalBackgroundCleanupPreview
	candidate, intent []byte
	docs ExposureSourceDocuments
	owner ExposureOwnerCosts
	limits ExposureResourceEstimate
	pin ExposureQualificationPin
	alpha v2input.Ref
}

// Own cleanup Params and numerical evidence are rebuilt with exact content hashes.
// Shared source geometry grants no physical or SQL authority.
func fullShownCleanupContentFixture(t *testing.T) shownCleanupContentFixture {
	t.Helper()
	child, approved, selected, physical := shownBackgroundCleanupCompileFixture(t)
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
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "background_cleanup", Reference: p.Source, ReferenceMask: arbitrary(), TargetSnapshotID: p.SnapshotID, Strength: .5, PreservePersons: true}
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
	evidence := BackgroundCleanupPreviewEvidence{SchemaVersion: 1, Params: p, WorkingImage: physical.linkage.Presentation.WorkingImage, Pixels: physical.linkage.Presentation.Pixels, Algorithm: "big_lama_work_srgb_delta_v1", ProtectedPixelsUnchanged: true, OutsideMaskUnchanged: true, GlobalPass: true, ChangedPixels: 0, ROI: []int64{0, 0, int64(image.Canvas.Width), int64(image.Canvas.Height)}}
	physical.linkage.Evidence.raw = encode(evidence)
	physical.linkage.Presentation.NumericalEvidence = ref(physical.linkage.Evidence.raw)
	physical.linkage.Document.raw = encode(physical.linkage.Presentation)
	physical.linkage.Document.Source.Ref = ref(physical.linkage.Document.raw)
	selected.Receipt.WorkingImage = physical.linkage.Presentation.WorkingImage
	selected.Receipt.NumericalEvidence = physical.linkage.Presentation.NumericalEvidence
	selected.Receipt.Presentation = physical.linkage.Document.Source.Ref
	_, selected.Receipt.ParamsSHA, e = FreezeBackgroundCleanupPreviewParams(p)
	if e != nil {
		t.Fatal(e)
	}
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: p.TargetID, Source: p.Source, Intent: p.Intent, Candidate: p.Candidate, Mask: maskRef, Transform: transform, Kind: "background_cleanup", Strength: .5, PreservePersons: true}
	approved.Body = encode(edit)
	approved.Params = ref(approved.Body)
	docs := ExposureSourceDocuments{Source: sourceBody, Reference: sourceBody, Mask: maskBody, Protection: protectedBody}
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: strings.Repeat("a", 64), Backend: "cpu", ModelLockSHA: p.ModelLockSHA, RuntimeLockSHA: p.RuntimeLockSHA, DeviceProfileSHA: p.DeviceProfileSHA, DatasetSHA: strings.Repeat("a", 64), Report: arbitrary(), Calibration: arbitrary()}
	return shownCleanupContentFixture{child: child, approved: approved, selected: selected, physical: physical,
		candidate: candidateBody, intent: intentBody, docs: docs, owner: owner, limits: limits, pin: pin, alpha: alpha}
}
