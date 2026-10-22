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

func shownBackgroundCleanupAdoptionFixture() ShownBackgroundCleanupAdoptionParams {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	p := ShownBackgroundCleanupAdoptionParams{SchemaVersion: 1, TenantID: uuid.New(), ChildRunID: uuid.New(), TargetID: uuid.New(),
		ApprovedEdit: ref(), PreviewRunID: uuid.New(), PreviewResultID: uuid.New(), PreviewAttempt: 2147483647, PreviewFence: 9223372036854775807,
		Presentation: ref(), NumericalEvidence: ref(), WorkingImage: ref(), Pixels: ref(), ModelLockSHA: strings.Repeat("b", 64), RuntimeLockSHA: strings.Repeat("c", 64), DeviceProfileSHA: strings.Repeat("d", 64)}
	p.PreviewTaskID = uuid.NewSHA1(p.PreviewRunID, []byte("semantic.preview_background_cleanup:"+p.TargetID.String()))
	return p
}
func TestShownBackgroundCleanupAdoptionFreezeKeepsExactProducerFence(t *testing.T) {
	p := shownBackgroundCleanupAdoptionFixture()
	raw, digest, err := FreezeShownBackgroundCleanupAdoptionParams(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong SHA")
	}
	var restored ShownBackgroundCleanupAdoptionParams
	if json.Unmarshal(raw, &restored) != nil || restored != p {
		t.Fatal("producer binding changed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 17 {
		t.Fatal("worker contract mismatch")
	}
}
func TestShownBackgroundCleanupAdoptionFreezeFailsWithoutPartialDocument(t *testing.T) {
	for _, mutate := range []func(*ShownBackgroundCleanupAdoptionParams){
		func(p *ShownBackgroundCleanupAdoptionParams) { p.ChildRunID = p.PreviewRunID },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.PreviewTaskID = uuid.New() },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.PreviewAttempt = 0 },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.PreviewFence = 0 },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.Pixels = p.WorkingImage },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.ApprovedEdit.ArtifactID = p.TenantID },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.Presentation.SHA256 = strings.Repeat("A", 64) },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.RuntimeLockSHA = "" },
		func(p *ShownBackgroundCleanupAdoptionParams) { p.SchemaVersion = 2 },
	} {
		p := shownBackgroundCleanupAdoptionFixture()
		mutate(&p)
		raw, digest, err := FreezeShownBackgroundCleanupAdoptionParams(p)
		if err == nil || raw != nil || digest != "" {
			t.Fatal("invalid copy input accepted")
		}
	}
}

func TestShownBackgroundCleanupAdoptionRejectsRemovalProducerNamespace(t *testing.T) {
	p := shownBackgroundCleanupAdoptionFixture()
	p.PreviewTaskID = uuid.NewSHA1(p.PreviewRunID, []byte("semantic.preview_remove_object:"+p.TargetID.String()))
	body, digest, err := FreezeShownBackgroundCleanupAdoptionParams(p)
	if err == nil || body != nil || digest != "" {
		t.Fatal("removal producer accepted for cleanup")
	}
}
