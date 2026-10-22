package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestApplyMaterializationExactParams(t *testing.T) {
	raw := json.RawMessage(`{"schema_version":1}`)
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	plan := applyNodePlan{ID: uuid.New(), NodeKey: "geometry.crop/0000", Operation: "geometry.crop", Params: raw, ParamsSHA: sha}
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: sha}
	reg := Registry{"geometry.crop": Operation{Version: "1", ResourceClass: "cpu", TimeoutSeconds: 300}}
	profile := json.RawMessage(`{"backend":"cpu"}`)
	got, err := prepareApplyMaterialization(reg, plan, ref, profile)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != plan.ID || got.Operation != plan.Operation || got.Params != ref || got.InputDigest == "" {
		t.Fatal("materialization identity changed")
	}
	profile[0] = ' '
	if got.ExecutionProfile[0] != '{' {
		t.Fatal("mutable profile alias")
	}
	for _, kind := range []string{"unknown_operation", "zero_id", "wrong_published_sha", "wrong_plan_sha", "noncanonical_params", "invalid_profile"} {
		t.Run(kind, func(t *testing.T) {
			p := plan
			r := ref
			v := json.RawMessage(`{"backend":"cpu"}`)
			switch kind {
			case "unknown_operation":
				p.Operation = "unknown"
			case "zero_id":
				p.ID = uuid.Nil
			case "wrong_published_sha":
				r.SHA256 = "wrong"
			case "wrong_plan_sha":
				p.ParamsSHA = "wrong"
			case "noncanonical_params":
				p.Params = json.RawMessage(`{ "schema_version":1}`)
			case "invalid_profile":
				v = json.RawMessage(`bad`)
			}
			if _, e := prepareApplyMaterialization(reg, p, r, v); e == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}
