package semanticreview

import (
	"bytes"
	"github.com/google/uuid"
	"os"
	"testing"
)

func TestShownRetouchAdoptionSharedWorkerWire(t *testing.T) {
	raw, err := os.ReadFile("testdata/retouch_preview_adoption_params.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseShownRetouchAdoptionParams(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.PreviewAttempt != 2147483647 || p.PreviewFence != 9223372036854775807 {
		t.Fatal("producer fence changed")
	}
	body, _, err := FreezeShownRetouchAdoptionParams(p)
	if err != nil || !bytes.Equal(body, raw) {
		t.Fatal("wire changed", err)
	}
	for _, mutate := range []func(*ShownRetouchAdoptionParams){
		func(p *ShownRetouchAdoptionParams) {
			p.PreviewTaskID = uuid.NewSHA1(p.PreviewRunID, []byte("semantic.preview_background_cleanup:"+p.TargetID.String()))
		},
		func(p *ShownRetouchAdoptionParams) { p.Pixels = p.WorkingImage },
		func(p *ShownRetouchAdoptionParams) { p.ChildRunID = p.PreviewRunID },
		func(p *ShownRetouchAdoptionParams) { p.PreviewAttempt = 0 },
		func(p *ShownRetouchAdoptionParams) { p.PreviewFence = 0 },
	} {
		q := p
		mutate(&q)
		if body, digest, err := FreezeShownRetouchAdoptionParams(q); err == nil || body != nil || digest != "" {
			t.Fatal("invalid producer frozen")
		}
	}
	for _, invalid := range [][]byte{append(append([]byte{}, raw...), ' '), bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"confirmed":true`), 1)} {
		if _, err := ParseShownRetouchAdoptionParams(invalid); err == nil {
			t.Fatal("accepted unfrozen bytes")
		}
	}
}
