package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeInputSavedParamsBindAllSourceTargetAndModelFields(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	binding := func() v2input.SessionVersionBinding {
		return v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	}
	expected := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: ref(), Reference: binding(), Target: binding(), ModelLockSHA: strings.Repeat("c", 64), Calibration: ref()}
	raw, _ := canonical.Marshal(expected)
	if err := verifyNativeInputSavedTransferParams(raw, expected); err != nil {
		t.Fatal("original params refused", err)
	}
	for _, mode := range []string{"intent", "reference", "target", "model", "calibration", "unknown"} {
		changed := expected
		switch mode {
		case "intent":
			changed.Intent = ref()
		case "reference":
			changed.Reference = binding()
		case "target":
			changed.Target = binding()
		case "model":
			changed.ModelLockSHA = strings.Repeat("d", 64)
		case "calibration":
			changed.Calibration = ref()
		}
		raw, _ := canonical.Marshal(changed)
		if mode == "unknown" {
			raw = append(raw[:len(raw)-1], []byte(",\"extra\":true}")...)
		}
		if err := verifyNativeInputSavedTransferParams(raw, expected); err == nil {
			t.Fatalf("accepted %s", mode)
		}
	}
}
