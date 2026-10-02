package workflow

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestBoundAnalysisOverridesKeepEachSourceDistinct(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	versionA, versionB := uuid.New(), uuid.New()
	shaA := bytes.Repeat([]byte{'a'}, 64)
	shaB := bytes.Repeat([]byte{'b'}, 64)
	body := func(sha []byte, gain float64) []byte {
		b, err := canonical.Marshal(map[string]any{
			"schema_version": 1, "revision": 1, "input_sha256": string(sha),
			"modules": map[string]any{"global": map[string]any{"gains_rgb": []float64{gain, 1, 1}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	items := []applyInput{
		{ItemID: first, VersionID: versionA, Image: styleArtifact{SHA256: string(shaA)}},
		{ItemID: second, VersionID: versionB, Image: styleArtifact{SHA256: string(shaB)}},
	}
	bound := []applyBoundItemOverride{
		{second, versionB, string(shaB), body(shaB, 1.1)},
		{first, versionA, string(shaA), body(shaA, 0.9)},
	}
	result, err := boundAnalysisOverrides(items, string(bytes.Repeat([]byte{'c'}, 64)), bound)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result[first], bound[1].Body) || !bytes.Equal(result[second], bound[0].Body) {
		t.Fatal("per-item corrections were reassigned")
	}
	wrong := append([]applyBoundItemOverride(nil), bound...)
	wrong[0].AssetVersionID = versionA
	if _, err := boundAnalysisOverrides(items, string(bytes.Repeat([]byte{'c'}, 64)), wrong); err == nil {
		t.Fatal("override bound to a different source version")
	}
	wrong = append([]applyBoundItemOverride(nil), bound...)
	wrong[0].Body = append([]byte{' '}, wrong[0].Body...)
	if _, err := boundAnalysisOverrides(items, string(bytes.Repeat([]byte{'c'}, 64)), wrong); err == nil {
		t.Fatal("noncanonical override accepted")
	}
}
