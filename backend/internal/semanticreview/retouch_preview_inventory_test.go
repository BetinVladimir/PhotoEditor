package semanticreview

import (
	stdcontext "context"
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

func TestRetouchInventoryFreezesContextAndFaceInputs(t *testing.T) {
	read := func(name string) []byte {
		raw, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	encode := func(value any) []byte {
		raw, e := canonical.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	hash := func(raw []byte) v2input.Ref {
		s := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(s[:])}
	}
	arbitrary := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	var images []json.RawMessage
	if json.Unmarshal(read("../review/testdata/working-images-python.json"), &images) != nil {
		t.Fatal("images")
	}
	image, e := review.ParseWorkingImage(images[0])
	if e != nil {
		t.Fatal(e)
	}
	canvas, e := canonical.Hash(image.Canvas)
	if e != nil {
		t.Fatal(e)
	}
	p := retouchParamsFixture()
	p.Source = v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: hash(images[0]), CanvasHash: canvas}
	manual := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
	maskBody := encode(manual)
	mask := hash(maskBody)
	protected := manual
	protected.Protected = true
	protected.Artifact.ID = uuid.New()
	protectedBody := encode(protected)
	p.Protection = hash(protectedBody)
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "retouch_region", Reference: p.Source, ReferenceMask: arbitrary(), TargetSnapshotID: p.SnapshotID, Strength: .5, PreservePersons: true}
	intentBody := encode(intent)
	p.Intent = hash(intentBody)
	candidateBody := encode(map[string]any{"schema_version": 1, "target": p.Source, "mask": mask, "transform": arbitrary(), "confidence": .9, "state": "proposed", "reason": nil})
	p.Candidate = hash(candidateBody)
	var context RetouchPreviewAdapterContext
	if json.Unmarshal(read("testdata/retouch_preview_context.json"), &context) != nil {
		t.Fatal("context")
	}
	context.Source = p.Source
	context.Candidate = p.Candidate
	context.Protection = p.Protection
	context.TransferredMask = mask
	context.Faces[0].Manual = maskBody
	contextBody := encode(context)
	contextRef := hash(contextBody)
	docs := ExposureSourceDocuments{Source: images[0], Reference: images[0], Mask: maskBody, Protection: protectedBody}
	run := uuid.New()
	for _, failure := range []string{"", "run", "workflow", "context_hash", "candidate_hash", "mask_bytes", "mask_alias"} {
		t.Run(failure, func(t *testing.T) {
			selectedRun := run
			selectedContext := contextRef
			selectedDocs := docs
			selectedCandidate := candidateBody
			selectedBody := contextBody
			switch failure {
			case "run":
				selectedRun = uuid.Nil
			case "workflow":
				selectedRun = p.WorkflowID
			case "context_hash":
				selectedContext.SHA256 = strings.Repeat("0", 64)
			case "candidate_hash":
				selectedCandidate = append(append([]byte{}, candidateBody...), ' ')
			case "mask_bytes":
				selectedDocs.Mask = append(append([]byte{}, maskBody...), ' ')
			case "mask_alias":
				c := context
				c.TransferredMask = arbitrary()
				selectedBody = encode(c)
				selectedContext = hash(selectedBody)
			}
			task, params, closure, e := CompileRetouchPreviewInventory(selectedRun, p, selectedCandidate, intentBody, selectedDocs, selectedContext, selectedBody)
			if failure != "" {
				if e == nil || task != uuid.Nil || params.ArtifactID != uuid.Nil {
					t.Fatal("invalid inventory accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			expected := uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+p.TargetID.String()))
			if task != expected || params.ArtifactID != uuid.NewSHA1(task, []byte("Params")) {
				t.Fatal("foreign namespace")
			}
			cursor, err := closure.Open(stdcontext.Background())
			if err != nil {
				t.Fatal(err)
			}
			var actual []v2input.Entry
			for {
				entry, more, err := cursor.Next(stdcontext.Background())
				if err != nil {
					t.Fatal(err)
				}
				if !more {
					break
				}
				actual = append(actual, entry)
			}
			wire := string(encode(actual))
			for _, ref := range []v2input.Ref{contextRef, context.Faces[0].Landmarks, mask} {
				if !strings.Contains(wire, ref.ArtifactID.String()) {
					t.Fatal("missing context dependency")
				}
			}
			pin := RetouchQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), Backend: "cpu", PolicySHA: p.ModelLockSHA, Report: arbitrary(), Calibration: arbitrary(), ModelLockSHA: p.ModelLockSHA, RuntimeLockSHA: p.RuntimeLockSHA, DeviceProfileSHA: p.DeviceProfileSHA, DatasetSHA: p.ModelLockSHA}
			owner := ExposureOwnerCosts{RuntimeRAMBytes: 1, InputIndexScratchBytes: 16 * 1048576}
			limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
			frozen, err := CompileRetouchPreviewTask(run, p, candidateBody, intentBody, docs, owner, limits, pin, contextRef, contextBody)
			if err != nil {
				t.Fatal(err)
			}
			published, err := PublishRetouchPreviewTask(stdcontext.Background(), frozen, &previewPublicationRecorder{})
			if err != nil {
				t.Fatal(err)
			}
			commit, err := PrepareRetouchPreviewCommitInputs(stdcontext.Background(), frozen, published)
			if err != nil {
				t.Fatal(err)
			}
			var transportInventory ExposureInventoryBinding
			if json.Unmarshal(commit.Inventory, &transportInventory) != nil || transportInventory.TaskID != task || len(transportInventory.Entries) != len(actual) {
				t.Fatal("compiler publication inventory mismatch")
			}
			draft, err := frozen.DraftTask()
			if err != nil || draft.ID != task || draft.Operation != "semantic.preview_retouch_region" || draft.ResourceClass != "cpu" {
				t.Fatal("wrong task", err)
			}
			var profile RetouchExecutionProfile
			if json.Unmarshal(draft.ExecutionProfile, &profile) != nil || profile.RetouchContext != contextRef || profile.Backend != "cpu" || profile.ResourceEstimate.VRAMBytes != 0 {
				t.Fatal("wrong context profile")
			}
			draft.ExecutionProfile[0] = '!'
			fresh, err := frozen.DraftTask()
			if err != nil || fresh.ExecutionProfile[0] != '{' {
				t.Fatal("mutable frozen task")
			}
			for _, rejection := range []string{"backend", "pin", "ram"} {
				qualified := pin
				budget := limits
				switch rejection {
				case "backend":
					qualified.Backend = "cuda"
				case "pin":
					qualified.RuntimeLockSHA = p.ModelLockSHA
				case "ram":
					budget.RAMBytes = 1
				}
				denied, err := CompileRetouchPreviewTask(run, p, candidateBody, intentBody, docs, owner, budget, qualified, contextRef, contextBody)
				if err == nil {
					t.Fatal("invalid task accepted", rejection)
				}
				if _, err := denied.DraftTask(); err == nil {
					t.Fatal("invalid task exposed")
				}
			}
			if !strings.Contains(wire, "RetouchPreviewAdapterContext") || !strings.Contains(wire, "Face68") {
				t.Fatal("missing contracts")
			}
		})
	}
}
