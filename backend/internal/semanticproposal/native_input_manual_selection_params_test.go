package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestNativeManualSelectionParamsRevalidatesOriginalSourceAndFullPolicy(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	body := images[0]
	sum := sha256.Sum256(body)
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	params, err := PrepareDefectParams(context.Background(), ref, body, []review.DefectProposalFaceInput{}, [2]float64{.003, .008}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: params.digest}
	policy, err := review.DefectProposalInputPolicyFromParams(review.ArtifactRef{ID: paramsRef.ArtifactID, SHA256: paramsRef.SHA256}, []byte(params.params), image.Source(ref))
	if err != nil {
		t.Fatal(err)
	}
	selection := nativeInputManualSelection{sourceBody: string(body), params: params, paramsRef: paramsRef, policy: policy}
	selection.binding.Source = v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	if err := verifyNativeInputManualSelectionParams(context.Background(), selection); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"source_bytes", "source_id", "policy", "unknown_params", "missing_inputs", "extra_inputs", "changed_roots", "null_roots"} {
		changed := selection
		switch field {
		case "source_bytes":
			changed.sourceBody += " "
		case "source_id":
			changed.binding.Source.ArtifactID = uuid.New()
		case "policy":
			changed.policy.Diameter = [2]float64{.002, .012}
		case "missing_inputs":
			changed.params.inputs = "[]"
		case "extra_inputs":
			var inputs []review.ArtifactRef
			if err := json.Unmarshal([]byte(params.inputs), &inputs); err != nil {
				t.Fatal(err)
			}
			inputs = append(inputs, review.ArtifactRef{ID: uuid.New(), SHA256: ref.SHA256})
			bytes, err := json.Marshal(inputs)
			if err != nil {
				t.Fatal(err)
			}
			changed.params.inputs = string(bytes)
		case "changed_roots":
			bytes, err := json.Marshal([]review.ArtifactRef{ref})
			if err != nil {
				t.Fatal(err)
			}
			changed.params.roots = string(bytes)
		case "null_roots":
			changed.params.roots = "null"
		case "unknown_params":
			changed.params.params = `{"unknown":true,` + params.params[1:]
			digest := sha256.Sum256([]byte(changed.params.params))
			changed.params.digest = hex.EncodeToString(digest[:])
			changed.paramsRef.SHA256 = changed.params.digest
		}
		if err := verifyNativeInputManualSelectionParams(context.Background(), changed); err == nil {
			t.Fatal("changed original accepted", field)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyNativeInputManualSelectionParams(ctx, selection); err != context.Canceled {
		t.Fatal(err)
	}
}

// Compiler replay with a manual target and its automatically derived protected
// child; this does not attest SQL producer provenance or physical raster pixels.
func TestNativeManualSelectionParamsPreservesManualClosure(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("image fixture")
	}
	body := images[0]
	digest := sha256.Sum256(body)
	imageRef := review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	rootBytes, err := os.ReadFile("../review/testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(rootBytes, &root) != nil {
		t.Fatal("manual fixture")
	}
	var target review.ProtectionMask
	if json.Unmarshal(root["target"], &target) != nil {
		t.Fatal("target fixture")
	}
	root["image"], err = json.Marshal(imageRef)
	if err != nil {
		t.Fatal(err)
	}
	rootBytes, err = json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	digest = sha256.Sum256(rootBytes)
	original := review.OriginalManualProtection{Root: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}, Body: rootBytes, Target: target}
	skin := target
	skin.Selector = "skin"
	skin.Artifact.ID = uuid.New()
	faces := []review.DefectProposalFaceInput{{Landmarks: review.ArtifactRef{ID: uuid.New(), SHA256: target.Artifact.SHA256}, ObjectID: target.ObjectID, Skin: &skin, Manual: &target}}
	manuals := []review.OriginalManualProtection{original}
	params, err := PrepareDefectParams(context.Background(), imageRef, body, faces, [2]float64{.003, .008}, nil, nil, manuals)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: params.digest}
	policy, err := review.DefectProposalInputPolicyFromParams(review.ArtifactRef{ID: paramsRef.ArtifactID, SHA256: paramsRef.SHA256}, []byte(params.params), image.Source(imageRef))
	if err != nil {
		t.Fatal(err)
	}
	selection := nativeInputManualSelection{sourceBody: string(body), params: params, paramsRef: paramsRef, policy: policy, manuals: manuals}
	selection.binding.Source = v2input.Ref{ArtifactID: imageRef.ID, SHA256: imageRef.SHA256}
	if len(policy.Protections) != 1 {
		t.Fatal("manual protected child omitted")
	}
	if err := verifyNativeInputManualSelectionParams(context.Background(), selection); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"missing_root", "changed_root", "changed_target", "missing_retained_root"} {
		changed := selection
		changed.manuals = append([]review.OriginalManualProtection(nil), manuals...)
		switch field {
		case "missing_root":
			changed.manuals = nil
		case "changed_root":
			changed.manuals[0].Body = append(append([]byte(nil), rootBytes...), ' ')
		case "changed_target":
			changed.manuals[0].Target.Artifact.ID = uuid.New()
		case "missing_retained_root":
			changed.params.roots = "[]"
		}
		if err := verifyNativeInputManualSelectionParams(context.Background(), changed); err == nil {
			t.Fatal("changed manual closure accepted", field)
		}
	}
}
