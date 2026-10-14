package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestReviewedDefectRepairParamsBindPhysicalImageAndProtection(t *testing.T) {
	imageFixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(imageFixture, &images) != nil || len(images) == 0 {
		t.Fatal("missing image fixture")
	}
	imageRaw, err := canonical.Bytes(images[0])
	if err != nil {
		t.Fatal(err)
	}
	imageHash := sha256.Sum256(imageRaw)
	var imageDoc map[string]any
	if json.Unmarshal(imageRaw, &imageDoc) != nil {
		t.Fatal("invalid image fixture")
	}
	paramsFixture, err := os.ReadFile("../review/testdata/defect-repair-params-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	if json.Unmarshal(paramsFixture, &params) != nil {
		t.Fatal("invalid repair fixture")
	}
	params["image"].(map[string]any)["sha256"] = hex.EncodeToString(imageHash[:])
	mask := params["protected"].(map[string]any)
	mask["canvas"] = imageDoc["canvas"]
	mask["transform_chain"] = imageDoc["transform_chain"]
	mask["protected"] = true
	paramsRaw, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	paramsHash := sha256.Sum256(paramsRaw)
	getRef := func(key string) v2input.Ref {
		body, marshalErr := json.Marshal(params[key])
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var item review.ArtifactRef
		if json.Unmarshal(body, &item) != nil {
			t.Fatal("invalid ref", key)
		}
		return v2input.Ref{ArtifactID: item.ID, SHA256: item.SHA256}
	}
	source := ReviewedDefectSource{Image: getRef("image"), Proposal: getRef("proposal"),
		Selection: getRef("review"), Resolved: getRef("resolved"),
		Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(paramsHash[:])}}
	actualMask, protection, err := DecodeReviewedDefectRepairParams(paramsRaw, imageRaw, source)
	if err != nil || !actualMask.Protected || !validManifestRef(protection) {
		t.Fatal("valid physical repair context rejected", err)
	}
	changed := source
	changed.Proposal.ArtifactID = uuid.New()
	if _, _, err := DecodeReviewedDefectRepairParams(paramsRaw, imageRaw, changed); err == nil {
		t.Fatal("substituted proposal accepted")
	}
	changed = source
	changed.Params.SHA256 = strings.Repeat("0", 64)
	if _, _, err := DecodeReviewedDefectRepairParams(paramsRaw, imageRaw, changed); err == nil {
		t.Fatal("changed Params bytes accepted")
	}
	if _, _, err := DecodeReviewedDefectRepairParams(append(paramsRaw, ' '), imageRaw, source); err == nil {
		t.Fatal("noncanonical Params accepted")
	}
	mask["protected"] = false
	unsafeParams, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	unsafeHash := sha256.Sum256(unsafeParams)
	changed = source
	changed.Params.SHA256 = hex.EncodeToString(unsafeHash[:])
	if _, _, err := DecodeReviewedDefectRepairParams(unsafeParams, imageRaw, changed); err == nil {
		t.Fatal("unprotected repair mask accepted")
	}
}
