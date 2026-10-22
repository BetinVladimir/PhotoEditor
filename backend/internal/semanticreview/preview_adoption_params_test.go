package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func shownAdoptionFixture() ShownExposureAdoptionParams {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	p := ShownExposureAdoptionParams{SchemaVersion: 1, TenantID: uuid.New(), ChildRunID: uuid.New(), TargetID: uuid.New(),
		ApprovedEdit: ref(), PreviewRunID: uuid.New(), PreviewResultID: uuid.New(), PreviewAttempt: 2147483647, PreviewFence: 9223372036854775807,
		Presentation: ref(), NumericalEvidence: ref(), WorkingImage: ref(), Pixels: ref(), ModelLockSHA: strings.Repeat("b", 64), RuntimeLockSHA: strings.Repeat("c", 64), DeviceProfileSHA: strings.Repeat("d", 64)}
	p.PreviewTaskID = uuid.NewSHA1(p.PreviewRunID, []byte("semantic.preview_local_exposure:"+p.TargetID.String()))
	return p
}
func TestShownAdoptionFreezeKeepsExactProducerFence(t *testing.T) {
	p := shownAdoptionFixture()
	raw, digest, err := FreezeShownExposureAdoptionParams(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong SHA")
	}
	var restored ShownExposureAdoptionParams
	if json.Unmarshal(raw, &restored) != nil || restored != p {
		t.Fatal("producer binding changed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 17 {
		t.Fatal("worker contract mismatch")
	}
}
func TestShownAdoptionFreezeFailsWithoutPartialDocument(t *testing.T) {
	for _, mutate := range []func(*ShownExposureAdoptionParams){
		func(p *ShownExposureAdoptionParams) { p.ChildRunID = p.PreviewRunID },
		func(p *ShownExposureAdoptionParams) { p.PreviewTaskID = uuid.New() },
		func(p *ShownExposureAdoptionParams) { p.PreviewAttempt = 0 },
		func(p *ShownExposureAdoptionParams) { p.PreviewFence = 0 },
		func(p *ShownExposureAdoptionParams) { p.Pixels = p.WorkingImage },
		func(p *ShownExposureAdoptionParams) { p.ApprovedEdit.ArtifactID = p.TenantID },
		func(p *ShownExposureAdoptionParams) { p.Presentation.SHA256 = strings.Repeat("A", 64) },
		func(p *ShownExposureAdoptionParams) { p.RuntimeLockSHA = "" },
		func(p *ShownExposureAdoptionParams) { p.SchemaVersion = 2 },
	} {
		p := shownAdoptionFixture()
		mutate(&p)
		raw, digest, err := FreezeShownExposureAdoptionParams(p)
		if err == nil || raw != nil || digest != "" {
			t.Fatal("invalid copy input accepted")
		}
	}
}
