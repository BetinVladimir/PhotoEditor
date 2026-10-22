package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestOriginalAuthoredDefectPolicyOwnsDecodedDeclarations(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Params     json.RawMessage `json:"params"`
		Protection struct {
			Canvas    review.Canvas   `json:"canvas"`
			Transform json.RawMessage `json:"transform_chain"`
		} `json:"protection"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var params struct {
		Image review.ArtifactRef `json:"image"`
	}
	if err = json.Unmarshal(doc.Params, &params); err != nil {
		t.Fatal(err)
	}
	body, err := canonical.Bytes(doc.Params)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	source := review.DefectSource{Image: params.Image, Canvas: doc.Protection.Canvas, TransformChain: doc.Protection.Transform}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	if _, err = review.DefectProposalInputPolicyFromParams(review.ArtifactRef{ID: ref.ArtifactID, SHA256: ref.SHA256}, body, source); err != nil {
		t.Fatal(err)
	}
	proof := OriginalAuthoredDefectPolicy{params: ref, paramsJCS: string(body), source: source}
	initial, err := canonical.Marshal(proof.Policy())
	if err != nil {
		t.Fatal(err)
	}
	selected := proof.Policy()
	if len(selected.Faces) == 0 {
		t.Fatal("fixture lacks face policy")
	}
	selected.Faces[0].Landmarks.ID = uuid.New()
	if selected.Permanent != nil {
		selected.Permanent.Artifact.ID = uuid.New()
	}
	if len(selected.Protections) > 0 {
		selected.Protections[0].Artifact.ID = uuid.New()
	}
	changedSource := proof.Source()
	if len(changedSource.TransformChain) > 0 {
		changedSource.TransformChain[0] = 'x'
	}
	again, err := canonical.Marshal(proof.Policy())
	if err != nil || string(initial) != string(again) {
		t.Fatal("policy getter leaked mutable declarations", err)
	}
	if string(proof.Source().TransformChain) != string(source.TransformChain) {
		t.Fatal("source getter leaked transform")
	}
	proof.params.SHA256 = strings.Repeat("0", 64)
	invalid := proof.Policy()
	if invalid.Diameter != ([2]float64{}) || invalid.Permanent != nil || len(invalid.Faces) != 0 {
		t.Fatal("changed Params pin accepted")
	}
}

func TestOriginalAuthoredDefectLocatorRejectsHostileMetadata(t *testing.T) {
	object := originalDefectPolicyObject{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bucket: "private", Key: "key", Version: "version", Bytes: 2}
	original := originalDefectPolicyLocator{Repair: object, Proposal: &object, Params: &object, Image: &object, Producer: &originalDefectPolicyProducer{Run: uuid.New(), Task: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1}}
	raw, err := canonical.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeOriginalDefectPolicyLocator(raw, "private", 2); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foreign_bucket", "missing_image", "unknown", "duplicate", "null", "wrong_stage", "missing_fence", "unversioned", "changed_sha"} {
		t.Run(name, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			bucket := "private"
			stage := 2
			switch name {
			case "foreign_bucket":
				bucket = "foreign"
			case "missing_image":
				delete(body, "image")
			case "unknown":
				body["invented"] = true
			case "missing_fence":
				body["producer"].(map[string]any)["fence"] = 0
			case "unversioned":
				body["image"].(map[string]any)["version"] = "null"
			case "changed_sha":
				body["params"].(map[string]any)["ref"].(map[string]any)["sha256"] = "x"
			case "wrong_stage":
				stage = 0
			}
			candidate, _ := json.Marshal(body)
			if name == "null" {
				candidate = []byte("null")
			}
			if name == "duplicate" {
				candidate = append([]byte(`{"repair":null,`), raw[1:]...)
			}
			if _, err := decodeOriginalDefectPolicyLocator(candidate, bucket, stage); err == nil {
				t.Fatal("hostile locator accepted")
			}
		})
	}
}

func TestOriginalAuthoredDefectReaderCancellationBeforeSQLOrObjects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLAuthoredOriginalDefectPolicyReader{}
	if _, err := reader.Read(ctx, auth.Identity{}, AuthoredInstructionSource{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := reader.Recheck(ctx, auth.Identity{}, AuthoredInstructionSource{}, OriginalAuthoredDefectPolicy{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestOriginalAuthoredDefectInstructionSnapshotOwnsLayers(t *testing.T) {
	layer := uuid.New()
	caller := AuthoredInstructionSource{Instruction: AuthoredGeometryInstruction{Layers: []uuid.UUID{layer}}}
	owned, wire, err := ownOriginalDefectInstructionSource(caller)
	if err != nil {
		t.Fatal(err)
	}
	caller.Instruction.Layers[0] = uuid.New()
	if owned.Instruction.Layers[0] != layer {
		t.Fatal("caller mutation changed IO snapshot")
	}
	owned.Instruction.Layers[0] = uuid.New()
	if owned.Instruction.Layers[0] == caller.Instruction.Layers[0] {
		t.Fatal("snapshot aliases caller")
	}
	if !strings.Contains(wire, layer.String()) {
		t.Fatal("snapshot bytes lost original layer")
	}
}
