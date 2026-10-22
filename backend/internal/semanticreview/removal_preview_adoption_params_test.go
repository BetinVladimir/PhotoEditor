package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func shownRemovalAdoptionFixture() ShownRemovalAdoptionParams {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	p := ShownRemovalAdoptionParams{SchemaVersion: 1, TenantID: uuid.New(), ChildRunID: uuid.New(), TargetID: uuid.New(),
		ApprovedEdit: ref(), PreviewRunID: uuid.New(), PreviewResultID: uuid.New(), PreviewAttempt: 2147483647, PreviewFence: 9223372036854775807,
		Presentation: ref(), NumericalEvidence: ref(), WorkingImage: ref(), Pixels: ref(), ModelLockSHA: strings.Repeat("b", 64), RuntimeLockSHA: strings.Repeat("c", 64), DeviceProfileSHA: strings.Repeat("d", 64)}
	p.PreviewTaskID = uuid.NewSHA1(p.PreviewRunID, []byte("semantic.preview_remove_object:"+p.TargetID.String()))
	return p
}
func TestShownRemovalAdoptionFreezeKeepsExactProducerFence(t *testing.T) {
	p := shownRemovalAdoptionFixture()
	raw, digest, err := FreezeShownRemovalAdoptionParams(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong SHA")
	}
	var restored ShownRemovalAdoptionParams
	if json.Unmarshal(raw, &restored) != nil || restored != p {
		t.Fatal("producer binding changed")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 17 {
		t.Fatal("worker contract mismatch")
	}
}
func TestShownRemovalAdoptionFreezeFailsWithoutPartialDocument(t *testing.T) {
	for _, mutate := range []func(*ShownRemovalAdoptionParams){
		func(p *ShownRemovalAdoptionParams) { p.ChildRunID = p.PreviewRunID },
		func(p *ShownRemovalAdoptionParams) { p.PreviewTaskID = uuid.New() },
		func(p *ShownRemovalAdoptionParams) { p.PreviewAttempt = 0 },
		func(p *ShownRemovalAdoptionParams) { p.PreviewFence = 0 },
		func(p *ShownRemovalAdoptionParams) { p.Pixels = p.WorkingImage },
		func(p *ShownRemovalAdoptionParams) { p.ApprovedEdit.ArtifactID = p.TenantID },
		func(p *ShownRemovalAdoptionParams) { p.Presentation.SHA256 = strings.Repeat("A", 64) },
		func(p *ShownRemovalAdoptionParams) { p.RuntimeLockSHA = "" },
		func(p *ShownRemovalAdoptionParams) { p.SchemaVersion = 2 },
	} {
		p := shownRemovalAdoptionFixture()
		mutate(&p)
		raw, digest, err := FreezeShownRemovalAdoptionParams(p)
		if err == nil || raw != nil || digest != "" {
			t.Fatal("invalid copy input accepted")
		}
	}
}

func TestShownRemovalExactParserRejectsForeignOrNoncanonicalWire(t *testing.T) {
 p := shownRemovalAdoptionFixture()
 p.PreviewFence = 9007199254740991
 raw, _, err := FreezeShownRemovalAdoptionParams(p)
 if err != nil { t.Fatal(err) }
 restored, err := ParseShownRemovalAdoptionParams(raw)
 if err != nil || restored != p { t.Fatal("exact wire changed", err) }
 for _, bad := range [][]byte{
  append(append([]byte{},raw...), ' '),
  bytes.Replace(raw, []byte(`"preview_attempt":2147483647`), []byte(`"preview_attempt":true`), 1),
  append(append([]byte{},raw[:len(raw)-1]...), []byte(`,"confirmed":true}`)...),
  bytes.Replace(raw, []byte(`"preview_fence":9007199254740991`), []byte(`"preview_fence":0`), 1),
 } {
  if _, err := ParseShownRemovalAdoptionParams(bad); err == nil { t.Fatal("invalid copy wire accepted") }
 }
}
