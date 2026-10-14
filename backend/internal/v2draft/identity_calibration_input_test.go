package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestIdentityCalibrationInputBindsExactParamsAndDataset(t *testing.T) {
	fixturesRaw, err := os.ReadFile("../../../docs/implementation-v2/contracts/structural-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if json.Unmarshal(fixturesRaw, &fixtures) != nil {
		t.Fatal("fixtures")
	}
	var values map[string]any
	if json.Unmarshal(fixtures.Fixtures["Calibration"], &values) != nil {
		t.Fatal("calibration")
	}
	values["qualified"] = true
	values["loss_weights"] = []float64{1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6}
	raw, err := canonical.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	object := objectstore.Object{Key: "calibration", VersionID: "v1", SHA256: ref.SHA256,
		Bytes: int64(len(raw)), ContentType: "application/json"}
	catalog := &identityInputCatalogFixture{items: map[string]objectstore.Object{"calibration": object}, reads: map[string]int{}}
	objects := restorationArrayReaderFixture{"calibration": raw}
	inputs := IdentityResultInputs{Params: v2input.IdentityCompareParams{Calibration: ref,
		ModelLockSHA: values["model_lock_sha"].(string)},
		ParamsRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}}
	scope := IdentityResultScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(),
		Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}}
	got, err := LoadIdentityCalibrationInput(context.Background(), scope, catalog, objects, inputs)
	if err != nil || got.Ref != ref || got.DatasetSHA != values["dataset_sha"] ||
		catalog.reads["calibration"] != 2 {
		t.Fatalf("calibration rejected: %+v %v", got, err)
	}
	objects["calibration"] = append([]byte(nil), raw...)
	objects["calibration"][len(raw)-1] ^= 1
	if _, err := LoadIdentityCalibrationInput(context.Background(), scope, catalog, objects, inputs); err == nil {
		t.Fatal("changed calibration bytes accepted")
	}
}
