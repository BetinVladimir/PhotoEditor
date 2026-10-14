package workflow

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDeliverySourceContractRequiresFrozenRecipeAndExactBinding(t *testing.T) {
	_, _, child, imageRaw, _ := deliveryExportPlanFixture(t, nil)
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		t.Fatal(err)
	}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	authored, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": child.Binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := v2draft.ResolvedRecipeInputs{Binding: child.Binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: image.Color.ICCSHA256, ModelLockSHA: strings.Repeat("f", 64)}
	frozen, err := v2draft.BuildFrozenRenderRecipe(authored, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDeliverySourceContracts(imageRaw, frozen, child.Binding, image.Color.ICCSHA256); err != nil {
		t.Fatal(err)
	}
	if err := validateDeliverySourceContracts(imageRaw, authored, child.Binding, image.Color.ICCSHA256); err == nil {
		t.Fatal("bare authored recipe accepted")
	}
	changed := child.Binding
	changed.AssetVersionID = uuid.New()
	if err := validateDeliverySourceContracts(imageRaw, frozen, changed, image.Color.ICCSHA256); err == nil {
		t.Fatal("foreign version accepted")
	}
	if err := validateDeliverySourceContracts(imageRaw, frozen, child.Binding, strings.Repeat("0", 64)); err == nil {
		t.Fatal("foreign deployment ICC accepted")
	}
	if err := validateDeliverySourceContracts(imageRaw, append(frozen, ' '), child.Binding, image.Color.ICCSHA256); err == nil {
		t.Fatal("noncanonical wrapper accepted")
	}
}
