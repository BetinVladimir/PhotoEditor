package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestBackgroundCleanupEvidenceBindsRequestPreservationAndROI(t *testing.T) {
	params := BackgroundCleanupPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}
	for _, mutation := range []string{"valid", "valid_large", "oversized", "params", "alias", "outside", "protected", "algorithm", "roi_empty", "roi_negative", "roi_reversed", "roi_small", "changed_negative", "changed_fraction", "global_missing", "global_null", "global_number", "hash", "unknown"} {
		t.Run(mutation, func(t *testing.T) {
			evidence := BackgroundCleanupPreviewEvidence{SchemaVersion: 1, Params: params, WorkingImage: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.Plan.SHA256}, Pixels: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.Plan.SHA256}, Algorithm: "big_lama_work_srgb_delta_v1", ProtectedPixelsUnchanged: true, OutsideMaskUnchanged: true, ChangedPixels: 4, ROI: []int64{0, 0, 2, 2}}
			if mutation == "params" {
				evidence.Params.ContextPixels++
			}
			if mutation == "alias" {
				evidence.Pixels = evidence.WorkingImage
			}
			raw, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "outside":
				fields["outside_mask_unchanged"] = json.RawMessage("false")
			case "protected":
				fields["protected_pixels_unchanged"] = json.RawMessage("false")
			case "algorithm":
				fields["algorithm"] = json.RawMessage(`"other"`)
			case "roi_empty":
				fields["roi"] = json.RawMessage(`[]`)
			case "roi_negative":
				fields["roi"] = json.RawMessage(`[-1,0,2,2]`)
			case "roi_reversed":
				fields["roi"] = json.RawMessage(`[2,0,1,2]`)
			case "roi_small":
				fields["roi"] = json.RawMessage(`[0,0,1,1]`)
			case "changed_negative":
				fields["changed_pixels"] = json.RawMessage(`-1`)
			case "changed_fraction":
				fields["changed_pixels"] = json.RawMessage(`1.5`)
			case "global_missing":
				delete(fields, "global_pass")
			case "global_null":
				fields["global_pass"] = json.RawMessage(`null`)
			case "global_number":
				fields["global_pass"] = json.RawMessage(`0`)
			case "unknown":
				fields["approval"] = json.RawMessage(`true`)
			}
			raw, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "valid_large" {
				raw = append(raw, bytes.Repeat([]byte(" "), 65537)...)
			}
			if mutation == "oversized" {
				raw = append(raw, bytes.Repeat([]byte(" "), 1048577)...)
			}
			sum := sha256.Sum256(raw)
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			if mutation == "hash" {
				raw = append(raw, ' ')
			}
			_, err = DecodeBackgroundCleanupPreviewEvidence(ref, raw, params)
			if (err == nil) != (mutation == "valid" || mutation == "valid_large") {
				t.Fatal(mutation, err)
			}
		})
	}
}
