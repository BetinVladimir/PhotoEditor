package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/exportpreset"
	"testing"
)

func TestDeliveryPresentationFrozenPresetStages(t *testing.T) {
	for _, kind := range []string{"resize", "sharpen", "both"} {
		t.Run(kind, func(t *testing.T) {
			plan, parent, child, raw, _ := deliveryExportPlanFixture(t, func(config *exportpreset.Config) {
				if kind != "sharpen" {
					edge := 200
					config.Size = &exportpreset.Size{LongEdge: &edge, Fit: "contain", AllowUpscale: false}
				}
				if kind != "resize" {
					config.Sharpen = &exportpreset.Sharpen{Radius: 1.2, Strength: .4, Threshold: .02}
				}
			})
			registry, err := LoadRegistry()
			if err != nil {
				t.Fatal(err)
			}
			node, err := CompileDeliveryPresentation(plan, parent, child, raw, registry)
			if err != nil || node == nil {
				t.Fatal("missing stage", err)
			}
			operation := "derivative.resize"
			if kind == "sharpen" {
				operation = "derivative.sharpen"
			}
			if node.Operation != operation || node.ID != uuid.NewSHA1(child.RunID, []byte(operation+"/0000000000000000")) {
				t.Fatal("stage order/identity")
			}
			again, err := CompileDeliveryPresentation(plan, parent, child, raw, registry)
			if err != nil || !bytes.Equal(node.Params, again.Params) {
				t.Fatal("unstable params")
			}
			var params map[string]json.RawMessage
			if json.Unmarshal(node.Params, &params) != nil {
				t.Fatal("invalid params")
			}
			if kind == "sharpen" {
				if string(params["max_delta"]) != "0.03" || string(params["algorithm"]) != `"luma_unsharp_v1"` || string(params["strength"]) != "0.4" {
					t.Fatal("sharpen settings lost")
				}
			} else if string(params["long_edge"]) != "200" || string(params["allow_upscale"]) != "false" {
				t.Fatal("resize settings lost")
			}
			limits := DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}
			profile, err := CompileDeliveryDerivativeProfile(*node, raw, limits)
			if err != nil {
				t.Fatal(err)
			}
			estimate, err := ParseResourceEstimate(profile)
			if err != nil || estimate.VRAMBytes != 0 || estimate.OutputBytes < 8<<20 {
				t.Fatal("invalid derivative profile", err)
			}
			expectedPixels := int64(200 * 100)
			if kind == "sharpen" {
				expectedPixels = 400 * 200
			}
			if estimate.PixelCount != expectedPixels {
				t.Fatal("wrong target dimensions")
			}
			for _, resource := range []string{"ram", "scratch", "output"} {
				low := limits
				switch resource {
				case "ram":
					low.RAMBytes = estimate.RAMBytes - 1
				case "scratch":
					low.ScratchBytes = estimate.ScratchBytes - 1
				case "output":
					low.OutputBytes = estimate.OutputBytes - 1
				}
				if _, err := CompileDeliveryDerivativeProfile(*node, raw, low); err == nil {
					t.Fatal("underreserved", resource)
				}
			}
			delete(registry, operation)
			if _, err := CompileDeliveryPresentation(plan, parent, child, raw, registry); err == nil {
				t.Fatal("missing capability accepted")
			}
		})
	}
}
