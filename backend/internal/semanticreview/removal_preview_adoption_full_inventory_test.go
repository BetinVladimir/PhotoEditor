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
type shownRemovalContentFixture struct {
	child uuid.UUID
	approved ApprovedTaskDraft
	selected LockedRemovalPreviewSelection
	physical PhysicalRemovalPreview
	candidate, intent []byte
	docs ExposureSourceDocuments
	owner ExposureOwnerCosts
	limits ExposureResourceEstimate
	pin ExposureQualificationPin
	alpha v2input.Ref
}

// Own content compilation fixture; it grants no physical or SQL authority.
func fullShownRemovalContentFixture(t *testing.T) shownRemovalContentFixture {
	t.Helper()
	child, approved, selected, physical := shownRemovalCompileFixture(t)
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
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: p.Source, ReferenceMask: arbitrary(), TargetSnapshotID: p.SnapshotID, Strength: .5, PreservePersons: true}
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
	evidence := RemovalPreviewEvidence{SchemaVersion: 1, Params: p, WorkingImage: physical.linkage.Presentation.WorkingImage, Pixels: physical.linkage.Presentation.Pixels, Algorithm: "big_lama_work_srgb_delta_v1", ProtectedPixelsUnchanged: true, OutsideMaskUnchanged: true, GlobalPass: true, ChangedPixels: 0, ROI: []int64{0, 0, int64(image.Canvas.Width), int64(image.Canvas.Height)}}
	physical.linkage.Evidence.raw = encode(evidence)
	physical.linkage.Presentation.NumericalEvidence = ref(physical.linkage.Evidence.raw)
	physical.linkage.Document.raw = encode(physical.linkage.Presentation)
	physical.linkage.Document.Source.Ref = ref(physical.linkage.Document.raw)
	selected.Receipt.WorkingImage = physical.linkage.Presentation.WorkingImage
	selected.Receipt.NumericalEvidence = physical.linkage.Presentation.NumericalEvidence
	selected.Receipt.Presentation = physical.linkage.Document.Source.Ref
	_, selected.Receipt.ParamsSHA, e = FreezeRemovalPreviewParams(p)
	if e != nil {
		t.Fatal(e)
	}
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: p.TargetID, Source: p.Source, Intent: p.Intent, Candidate: p.Candidate, Mask: maskRef, Transform: transform, Kind: "remove_object", Strength: .5, PreservePersons: true}
	approved.Body = encode(edit)
	approved.Params = ref(approved.Body)
	docs := ExposureSourceDocuments{Source: sourceBody, Reference: sourceBody, Mask: maskBody, Protection: protectedBody}
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1024 * 1024}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: strings.Repeat("a", 64), Backend: "cpu", ModelLockSHA: p.ModelLockSHA, RuntimeLockSHA: p.RuntimeLockSHA, DeviceProfileSHA: p.DeviceProfileSHA, DatasetSHA: strings.Repeat("a", 64), Report: arbitrary(), Calibration: arbitrary()}
	return shownRemovalContentFixture{child: child, approved: approved, selected: selected, physical: physical,
		candidate: candidateBody, intent: intentBody, docs: docs, owner: owner, limits: limits, pin: pin, alpha: alpha}
}

func TestShownRemovalFullContentCompilesOwnCopyClosure(t *testing.T) {
 f:=fullShownRemovalContentFixture(t)
 compiled,err:=CompileShownRemovalAdoption(f.child,f.approved,f.selected,f.physical)
 if err!=nil {t.Fatal("own copy compilation",err)}
 frozen,err:=CompileShownRemovalTask(compiled,f.approved,f.physical,f.candidate,f.intent,f.docs,f.owner,f.limits,f.pin)
 if err!=nil {t.Fatal("full copy closure",err)}
 draft,err:=frozen.DraftTask()
 if err!=nil || draft.Operation!="semantic.adopt_shown_removal" || draft.ResourceClass!="cpu" {t.Fatal("wrong task",draft,err)}
}

func TestShownRemovalFullContentPublicationAndRefusal(t *testing.T) {
 for _,mode:=range []string{"valid","denied_before","denied_after","factory","cancel_before_factory","upload"} {
  t.Run(mode,func(t *testing.T){
   f:=fullShownRemovalContentFixture(t)
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   recorder:=&previewPublicationRecorder{}
   if mode=="upload" {recorder.failure="error"}
   checks,factories:=0,0
   got,err:=PrepareShownRemovalWithPublisher(ctx,f.child,f.approved,f.selected,f.physical,f.candidate,f.intent,f.docs,f.owner,f.limits,f.pin,
    func(FrozenShownRemovalTask)(v2input.Publisher,error){factories++;if mode=="factory" {return nil,ErrUnavailable};return recorder,nil},
    func(context.Context)error{checks++;if mode=="denied_before" && checks==1 || mode=="denied_after" && checks==2 {return ErrConflict};if mode=="cancel_before_factory" && checks==1 {cancel()};return nil})
   if mode=="valid" {
    if err!=nil || checks!=2 || factories!=1 || got.payload.Node==nil || len(recorder.calls)==0 {t.Fatal("full removal publication refused",err,checks,factories)}
   } else {
    if err==nil || got.payload.Node!=nil || got.documents!=nil {t.Fatal("failed publication escaped",mode,err)}
    if (mode=="denied_before" || mode=="cancel_before_factory") && (factories!=0 || len(recorder.calls)!=0) {t.Fatal("publication after authority loss")}
    if mode=="factory" && len(recorder.calls)!=0 {t.Fatal("upload after factory error")}
   }
  })
 }
}
