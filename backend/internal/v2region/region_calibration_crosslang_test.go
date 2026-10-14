package v2region

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func TestRegionCalibrationPythonWireParity(t *testing.T) {
	raw := os.Getenv("PHOTO_REGION_CALIBRATION_CASES")
	if raw == "" {
		t.Skip("Python generated wire vectors required")
	}
	var cases []struct{ Catalog, Profile, Params, Source json.RawMessage }
	if json.Unmarshal([]byte(raw), &cases) != nil || len(cases) != 2 {
		t.Fatal("opaque/alpha vectors required")
	}
	for _, c := range cases {
		var doc regionPlanDocument
		if json.Unmarshal(c.Params, &doc) != nil {
			t.Fatal("invalid Params")
		}
		source, err := review.ParseWorkingImage(c.Source)
		if err != nil {
			t.Fatal(err)
		}
		body, err := canonical.Bytes(c.Params)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		plan := FrozenRegionPlan{body: body, digest: hex.EncodeToString(sum[:]), canvas: source.Canvas, hasAlpha: source.Alpha != nil}
		var profile struct {
			Pin RegionCalibrationPin `json:"region_qualification"`
		}
		if json.Unmarshal(c.Profile, &profile) != nil {
			t.Fatal("invalid Profile")
		}
		catalog, err := NewPinnedRegionCalibrationCatalog(c.Catalog, profile.Pin.CatalogSHA)
		if err != nil {
			t.Fatal(err)
		}
		cost, err := catalog.Select(context.Background(), profile.Pin.Qualification, plan)
		if err != nil {
			t.Fatal(err)
		}
		if cost.Pin != profile.Pin {
			t.Fatal("Go/Python workload/profile/sample hash mismatch", cost.Pin, profile.Pin)
		}
		policy, err := CompileRegionExecutionPolicy(plan, cost)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(policy.Profile, c.Profile) {
			t.Fatal("Go/Python full resource/qualification wire mismatch", string(policy.Profile), string(c.Profile))
		}
	}
}
