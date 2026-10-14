package semanticproposal

import (
	"github.com/google/uuid"
	"testing"
)

func TestTransferSourceRequiresExactDetectorObjectsAcrossValidationModes(t *testing.T) {
	detector := sourcePreparationProof{tenant: uuid.New(), project: uuid.New(), actor: uuid.New(), session: uuid.New(), bucket: "owned", pins: "original versions", iccSHA: "original ICC", unitRGB: true}
	source := detector
	source.unitRGB = false
	if !nativeTransferSourceMatchesDetector(source, detector) || !detector.unitRGB {
		t.Fatal("same objects changed original detector proof")
	}
	for _, kind := range []string{"tenant", "project", "actor", "session", "bucket", "pins", "icc", "binding", "mode"} {
		changed := source
		switch kind {
		case "tenant":
			changed.tenant = uuid.New()
		case "project":
			changed.project = uuid.New()
		case "actor":
			changed.actor = uuid.New()
		case "session":
			changed.session = uuid.New()
		case "bucket":
			changed.bucket = "foreign"
		case "pins":
			changed.pins = "substituted version"
		case "icc":
			changed.iccSHA = "different ICC"
		case "binding":
			changed.binding.AssetVersionID = uuid.New()
		case "mode":
			changed.unitRGB = true
		}
		if nativeTransferSourceMatchesDetector(changed, detector) {
			t.Fatal("different source accepted", kind)
		}
	}
}
