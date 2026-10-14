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

func TestExposureDocumentsDeriveInputsAndRejectSubstitutedBytes(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	json.Unmarshal(raw, &fixtures)
	imageBody := fixtures[0]
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		t.Fatal(err)
	}
	hashRef := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	canvas, _ := canonical.Hash(image.Canvas)
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: hashRef(imageBody), CanvasHash: canvas}
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: false}
	maskBody, _ := canonical.Marshal(mask)
	maskRef := hashRef(maskBody)
	mask.Protected = true
	protectionBody, _ := canonical.Marshal(mask)
	protection := hashRef(protectionBody)
	arbitrary := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	ev := .25
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: uuid.New(), Source: source, Intent: arbitrary(), Candidate: arbitrary(), Mask: maskRef, Transform: arbitrary(), Kind: "local_exposure", Strength: .5, EV: &ev, PreservePersons: true}
	child := uuid.New()
	task := uuid.NewSHA1(child, []byte("semantic.edit:"+edit.TargetID.String()))
	body, _ := canonical.Marshal(edit)
	params := hashRef(body)
	params.ArtifactID = uuid.NewSHA1(task, []byte("Params"))
	draft := ApprovedTaskDraft{TaskID: task, TargetID: edit.TargetID, Params: params, Body: body, Kind: edit.Kind}
	sha := strings.Repeat("a", 64)
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: sha, Report: arbitrary(), Calibration: arbitrary(), ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, IssuerID: uuid.New(), Backend: "cpu"}
	docs := ExposureSourceDocuments{Source: imageBody, Reference: imageBody, Mask: maskBody, Protection: protectionBody}
	owner := ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}
	resolved, resolveErr := ResolveExposureTaskInputsFromDocuments(draft, source, protection, docs, owner, limits, pin)
	if resolveErr != nil || resolved.Source != source || resolved.Reference != source || resolved.Dependencies.Source.Pixels.ArtifactID != image.Pixels.ID || resolved.Dependencies.Protection != protection {
		t.Fatal("document input projection", resolveErr)
	}
	badDraft := draft
	badDraft.Params.SHA256 = strings.Repeat("0", 64)
	if _, err := ResolveExposureTaskInputsFromDocuments(badDraft, source, protection, docs, owner, limits, pin); err == nil {
		t.Fatal("changed Params pin accepted")
	}
	compiled, err := CompileExposureTaskFromDocuments(child, draft, source, protection, docs, owner, limits, pin)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := compiled.DraftTask()
	var profile ExposureExecutionProfile
	json.Unmarshal(node.ExecutionProfile, &profile)
	if profile.ResourceEstimate.PixelCount != int64(image.Canvas.Width)*int64(image.Canvas.Height) {
		t.Fatal("dimensions not derived")
	}
	for _, field := range []string{"source", "reference", "mask", "protection"} {
		changed := docs
		switch field {
		case "source":
			changed.Source = append(append([]byte(nil), imageBody...), ' ')
		case "reference":
			changed.Reference = append(append([]byte(nil), imageBody...), ' ')
		case "mask":
			changed.Mask = protectionBody
		case "protection":
			changed.Protection = maskBody
		}
		if _, err := CompileExposureTaskFromDocuments(child, draft, source, protection, changed, owner, limits, pin); err == nil {
			t.Fatal("substituted document accepted", field)
		}
	}
	if _, err := CompileExposureTaskFromDocuments(child, draft, source, maskRef, ExposureSourceDocuments{Source: imageBody, Reference: imageBody, Mask: maskBody, Protection: maskBody}, owner, limits, pin); err == nil {
		t.Fatal("unprotected root accepted")
	}
}

func TestExposureDocumentBatchRequiresExactApprovalCoverage(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	child := uuid.New()
	for _, documents := range [][]ExposureTargetDocuments{nil, {{TargetID: uuid.New()}}, {{TargetID: targets[0].ID}, {TargetID: targets[0].ID}}} {
		result, err := ResolveApprovedExposureDocumentInputs(child, prepared, targets, request, documents)
		if err == nil || result != nil {
			t.Fatal("invalid batch document coverage accepted")
		}
	}
	request.TargetDecisions[0].Decision = "reject"
	result, err := ResolveApprovedExposureDocumentInputs(child, prepared, targets, request, nil)
	if err != nil || len(result) != 0 {
		t.Fatal("rejection resolved image documents", err)
	}
}
