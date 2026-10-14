package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"testing"
)

func TestNativeSourceScopeRequiresOriginalSourceAndDeployedICC(t *testing.T) {
	m := manualPinTestProof()
	source := sourcePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, iccSHA: m.binding.Source.SHA256}
	root := m.objects.Root.object
	root.SHA256 = m.binding.Source.SHA256
	pins := []sourcePreparationPin{{v2draft.LayerRef{Ref: m.binding.Source, Contract: "WorkingImage", CanvasHash: m.binding.CanvasHash}, root}, {v2draft.LayerRef{Ref: m.objects.Pixel.ref, Contract: "WorkingPixels", ParentRef: m.binding.Source}, m.objects.Pixel.object}}
	raw, err := canonical.Marshal(pins)
	if err != nil {
		t.Fatal(err)
	}
	source.pins = string(raw)
	native := nativeReferenceContinuationProof{}
	native.defect.parent.result.origin = defectOriginPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: source.bucket, binding: m.binding}
	color := TransferColorProfile{sha: source.iccSHA}
	if err := matchNativeSourceScope(source, native, color); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"tenant", "project", "actor", "session", "bucket", "version", "canvas", "icc", "range", "closure"} {
		changed := source
		switch mode {
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
		case "version":
			changed.binding.AssetVersionID = uuid.New()
		case "canvas":
			changed.binding.CanvasHash = "foreign"
		case "icc":
			changed.iccSHA = "foreign"
		case "range":
			changed.unitRGB = true
		case "closure":
			changed.pins = "[]"
		}
		if err := matchNativeSourceScope(changed, native, color); err == nil {
			t.Fatal("mixed native source admitted", mode)
		}
	}
}
