package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestDefectActivationContentRetainsExactPinnedBytes(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(images[0])
	canvas, _ := canonical.Hash(image.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(context.Background(), review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}, images[0], []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	original := v2input.Ref{ArtifactID: uuid.New(), SHA256: compiled.ParamsSHA256}
	plan := defectPreparationDraftPlan{request: Request{Intent: v2input.SemanticEditIntent{Reference: binding}}, originalParams: string(compiled.Params), workingImage: string(images[0])}
	plan.execution.execution.face.origin.Params = original
	plan.execution.execution.face.originProof.objects[0].ref = original
	plan.params.params = v2input.Ref{ArtifactID: uuid.New(), SHA256: compiled.ParamsSHA256}
	plan.params.preparation.params.params = string(compiled.Params)
	pixels := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
	entries := []v2input.Entry{{Ref: plan.params.params, Contract: "Params"}, {Ref: binding.Source, Contract: "WorkingImage"}, {Ref: pixels, Contract: "NpyArray"}}
	sizes := []v2input.SemanticInputSize{{Ref: plan.params.params, Bytes: int64(len(compiled.Params))}, {Ref: binding.Source, Bytes: int64(len(images[0]))}, {Ref: pixels, Bytes: 12*image.Canvas.Width*image.Canvas.Height + 128}}
	plan.frozen, err = v2input.CompileSemanticDefectTask(context.Background(), uuid.New(), 0, binding, images[0], compiled.Params, plan.params.params, entries, sizes, v2input.SemanticQualifiedCosts{}, 1<<40, 0, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	published, err := v2input.PublishSemanticDefectTask(context.Background(), plan.frozen, &proposalMemoryTransport{})
	if err != nil {
		t.Fatal(err)
	}
	registrationBody, err := marshalDefectRegistrationBinding(context.Background(), plan, published.Graph.Built)
	if err != nil {
		t.Fatal(err)
	}
	var registration defectRegistrationBinding
	if json.Unmarshal(registrationBody, &registration) != nil || registration.TaskID != published.Graph.ID || registration.Params != plan.params.params || registration.InputDigest != published.InputDigest || registration.Transport.Count != 3 || registration.Transport.EntriesSHA != published.Graph.Built.SHA256 {
		t.Fatal("registration binding changed task or transport")
	}
	wrongDigest := published.Graph.Built
	wrongDigest.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if body, err := marshalDefectRegistrationBinding(context.Background(), plan, wrongDigest); err == nil || body != nil {
		t.Fatal("registration binding accepted changed entry digest", err)
	}
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := (SQLTransferSourceReader{}).lockRegisteredDefectPersonAdmission(context.Background(), tx, auth.Identity{}, defectPersonAdmissionPreparation{plan: plan}, v2input.Built{}); err == nil || len(tx.calls) != 0 {
		t.Fatal("missing published transport reached admission SQL", err, tx.calls)
	}
	content, err := marshalDefectActivationContent(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	filePins := []map[string]any{
		{"artifact_id": original.ArtifactID, "sha256": original.SHA256, "contract": "Params"},
		{"artifact_id": binding.Source.ArtifactID, "sha256": binding.Source.SHA256, "contract": "WorkingImage"},
		{"artifact_id": pixels.ArtifactID, "sha256": pixels.SHA256, "contract": "WorkingPixels"},
	}
	filesBody, _ := json.Marshal(filePins)
	if err := validateDefectAdmissionClosure(filesBody, content, registrationBody); err != nil {
		t.Fatal("valid retained closure rejected", err)
	}
	dependent := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	childFiles := append(append([]map[string]any(nil), filePins...), map[string]any{"artifact_id": dependent.ArtifactID, "sha256": dependent.SHA256, "contract": "DenseWarpField", "parent_id": binding.Source.ArtifactID, "parent_sha": binding.Source.SHA256})
	childFilesBody, _ := json.Marshal(childFiles)
	if err := validateDefectAdmissionClosure(childFilesBody, content, registrationBody); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("missing selected document child accepted", err)
	}
	withChild := registration
	var childRows []v2input.Entry
	if err := json.Unmarshal(withChild.Transport.Rows, &childRows); err != nil {
		t.Fatal(err)
	}
	childRows = append(childRows, v2input.Entry{Ref: dependent, Contract: "NpyArray"})
	sort.Slice(childRows, func(i, j int) bool {
		return childRows[i].Ref.ArtifactID.String() < childRows[j].Ref.ArtifactID.String()
	})
	withChild.Transport.EntriesSHA, _ = canonical.Hash(childRows)
	withChild.InputDigest, _ = canonical.Hash(map[string]any{"task": withChild.TaskID, "operation": "defects.propose", "version": "1", "entries": childRows})
	withChild.Transport.Rows, _ = json.Marshal(childRows)
	withChild.Transport.Count++
	withChildBody, _ := json.Marshal(withChild)
	if err := validateDefectAdmissionClosure(childFilesBody, content, withChildBody); err != nil {
		t.Fatal("complete selected geometry child rejected", err)
	}
	childFiles[len(childFiles)-1]["parent_sha"] = dependent.SHA256
	childFilesBody, _ = json.Marshal(childFiles)
	if err := validateDefectAdmissionClosure(childFilesBody, content, withChildBody); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("substituted parent SHA accepted", err)
	}
	// Native masks may bind an NPY artifact directly. Catalog/protection authority
	// remains independent; this exercises the declared-input contract only.
	maskPixels := review.ArtifactRef{ID: uuid.New(), SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	mask := review.ProtectionMask{Artifact: maskPixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "protected", Confidence: 1, Protected: true}
	masked, err := review.BuildDefectProposalTaskInputsFromImage(context.Background(), review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}, images[0], []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, &mask, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var maskedContent defectActivationContent
	if err := json.Unmarshal(content, &maskedContent); err != nil {
		t.Fatal(err)
	}
	maskedContent.Params.SHA256 = masked.ParamsSHA256
	maskedContent.ParamsText = string(masked.Params)
	maskedBody, _ := json.Marshal(maskedContent)
	maskedRegistration := registration
	maskedRegistration.Params = maskedContent.Params
	var maskedRows []v2input.Entry
	json.Unmarshal(registration.Transport.Rows, &maskedRows)
	for index := range maskedRows {
		if maskedRows[index].Contract == "Params" {
			maskedRows[index].Ref = maskedContent.Params
		}
	}
	maskedRows = append(maskedRows, v2input.Entry{Ref: v2input.Ref{ArtifactID: maskPixels.ID, SHA256: maskPixels.SHA256}, Contract: "NpyArray"})
	maskedRegistration.Transport.Count++
	sort.Slice(maskedRows, func(i, j int) bool {
		return maskedRows[i].Ref.ArtifactID.String() < maskedRows[j].Ref.ArtifactID.String()
	})
	maskedRegistration.Transport.EntriesSHA, _ = canonical.Hash(maskedRows)
	maskedRegistration.InputDigest, _ = canonical.Hash(map[string]any{"task": maskedRegistration.TaskID, "operation": "defects.propose", "version": "1", "entries": maskedRows})
	maskedRegistration.Transport.Rows, _ = json.Marshal(maskedRows)
	var maskedDirect []v2input.Ref
	json.Unmarshal(registration.Transport.Direct, &maskedDirect)
	for index := range maskedDirect {
		if maskedDirect[index].ArtifactID == maskedContent.Params.ArtifactID {
			maskedDirect[index] = maskedContent.Params
		}
	}
	maskedRegistration.Transport.Direct, _ = json.Marshal(maskedDirect)
	maskedRegistrationBody, _ := json.Marshal(maskedRegistration)
	maskedFiles := append(append([]map[string]any(nil), filePins...), map[string]any{"artifact_id": maskPixels.ID, "sha256": maskPixels.SHA256, "contract": "MaskPixels"})
	maskedFilesBody, _ := json.Marshal(maskedFiles)
	if err := validateDefectAdmissionClosure(maskedFilesBody, maskedBody, maskedRegistrationBody); err != nil {
		t.Fatal("native NPY mask input rejected", err)
	}
	for changed := 0; changed < 8; changed++ {
		var candidate defectRegistrationBinding
		if err := json.Unmarshal(registrationBody, &candidate); err != nil {
			t.Fatal(err)
		}
		var rows []v2input.Entry
		if err := json.Unmarshal(candidate.Transport.Rows, &rows); err != nil {
			t.Fatal(err)
		}
		switch changed {
		case 0:
			rows[1].Ref.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
		case 1:
			rows[1].Contract = "MaskRef"
		case 2:
			rows = rows[:len(rows)-1]
			candidate.Transport.Count--
		case 3:
			candidate.Transport.Direct, _ = json.Marshal([]v2input.Ref{pixels, pixels, plan.params.params})
		case 4:
			candidate.Params.ArtifactID = uuid.New()
		case 5:
			candidate.Transport.EntriesSHA = "0000000000000000000000000000000000000000000000000000000000000000"
		case 6:
			candidate.InputDigest = "0000000000000000000000000000000000000000000000000000000000000000"
		case 7:
			rows[0], rows[1] = rows[1], rows[0]
		}
		candidate.Transport.Rows, _ = json.Marshal(rows)
		changedBody, _ := json.Marshal(candidate)
		if err := validateDefectAdmissionClosure(filesBody, content, changedBody); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("substituted retained closure accepted", changed, err)
		}
	}
	// SQL provenance must never consume a caller-substituted envelope, even if
	// it decodes to similar JSON. It uses the exact private canonical envelope.
	for _, changed := range []json.RawMessage{nil, []byte("{}"), append(append([]byte(nil), content...), ' ')} {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
		err := (SQLTransferSourceReader{}).lockDefectContentProvenance(context.Background(), tx, auth.Identity{}, plan, changed)
		if err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("substituted provenance envelope reached SQL", err, tx.calls)
		}
	}
	var decoded defectActivationContent
	if json.Unmarshal(content, &decoded) != nil || decoded.WorkingImageText != plan.workingImage || decoded.OriginalParamsText != plan.originalParams || decoded.ParamsText != plan.params.preparation.params.params {
		t.Fatal("pinned text changed in JSON transport")
	}
	for changed := 0; changed < 4; changed++ {
		candidate := plan
		switch changed {
		case 0:
			candidate.workingImage += " "
		case 1:
			candidate.originalParams = "\n" + candidate.originalParams
		case 2:
			candidate.params.preparation.params.params += " "
		case 3:
			candidate.request.Intent.Reference.CanvasHash = "0000000000000000000000000000000000000000000000000000000000000000"
		}
		if body, err := marshalDefectActivationContent(context.Background(), candidate); err == nil || body != nil {
			t.Fatal("changed activation content accepted", changed, err)
		}
	}
	aliased := plan
	aliased.params.params = original
	if body, err := marshalDefectActivationContent(context.Background(), aliased); err == nil || body != nil {
		t.Fatal("original and native Params reused the same artifact identity", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if body, err := marshalDefectActivationContent(ctx, plan); err != context.Canceled || body != nil {
		t.Fatal("cancelled activation content accepted", err)
	}
}
