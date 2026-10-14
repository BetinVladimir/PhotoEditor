package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"testing"
)

func TestSourceClosureDecoderBeforeCatalog(t *testing.T) {
	m := manualPinTestProof()
	p := sourcePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, iccSHA: m.binding.Source.SHA256, unitRGB: true}
	root := m.objects.Root.object
	root.SHA256 = m.binding.Source.SHA256
	pins := []sourcePreparationPin{{v2draft.LayerRef{Ref: m.binding.Source, Contract: "WorkingImage", CanvasHash: m.binding.CanvasHash}, root}, {v2draft.LayerRef{Ref: m.objects.Pixel.ref, Contract: "WorkingPixels", ParentRef: m.binding.Source}, m.objects.Pixel.object}}
	raw, _ := canonical.Marshal(pins)
	p.pins = string(raw)
	if got, err := decodeSourcePreparationPins(p); err != nil || len(got) != 2 {
		t.Fatal(err)
	}
	mutations := []func([]sourcePreparationPin){
		func(v []sourcePreparationPin) { v[0].Node.Contract = "WorkingPixels" },
		func(v []sourcePreparationPin) { v[1].Node.ParentRef.ArtifactID = uuid.New() },
		func(v []sourcePreparationPin) { v[1].Object.VersionID = "null" },
		func(v []sourcePreparationPin) { v[1].Object.SHA256 = m.binding.Source.SHA256 },
		func(v []sourcePreparationPin) { v[1].Node.ExpectedBinding = &m.binding },
	}
	for _, mutate := range mutations {
		changed := append([]sourcePreparationPin(nil), pins...)
		mutate(changed)
		raw, _ := canonical.Marshal(changed)
		bad := p
		bad.pins = string(raw)
		if _, err := decodeSourcePreparationPins(bad); err == nil {
			t.Fatal("malformed closure decoded")
		}
		reader := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
		err := reader.recheckSourcePreparationProof(context.Background(), auth.Identity{UserID: m.actor, SessionID: m.session}, m.tenant, m.binding, TransferColorProfile{sha: p.iccSHA}, true, bad)
		if err == nil {
			t.Fatal("invalid source closure reached catalog")
		}
	}
}
