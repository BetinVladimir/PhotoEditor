package semanticproposal

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestDefectConfigCompatibilityPreservesOnlyExactLegacySnapshot(t *testing.T) {
	current := []byte(`{"project_id":"p","v27_defect_prepare":{"request_sha":"request","source":{"artifact_id":"source"},"original_producer":{"producer_task_id":"task","params_sha256":"params"}}}`)
	legacy := []byte(`{"project_id":"p","v27_defect_prepare":{"request_sha":"request","source":{"artifact_id":"source"}}}`)
	for _, saved := range [][]byte{current, legacy} {
		sha, err := canonical.Hash(json.RawMessage(saved))
		if err != nil {
			t.Fatal(err)
		}
		got, err := compatibleDefectConfig(current, saved, sha)
		want, canonErr := canonical.Bytes(saved)
		if err != nil || canonErr != nil || !bytes.Equal(got, want) {
			t.Fatal("immutable snapshot changed", err)
		}
	}
	for _, saved := range [][]byte{
		[]byte(`{"project_id":"other","v27_defect_prepare":{"request_sha":"request","source":{"artifact_id":"source"}}}`),
		[]byte(`{"project_id":"p","v27_defect_prepare":{"request_sha":"other","source":{"artifact_id":"source"}}}`),
		[]byte(`{"project_id":"p","v27_defect_prepare":{"request_sha":"request","source":{"artifact_id":"other"}}}`),
		[]byte(`{"project_id":"p","v27_defect_prepare":{"request_sha":"request","source":{"artifact_id":"source"},"original_producer":null}}`),
	} {
		sha, err := canonical.Hash(json.RawMessage(saved))
		if err != nil {
			t.Fatal(err)
		}
		got, err := compatibleDefectConfig(current, saved, sha)
		if err == nil || got != nil {
			t.Fatal("snapshot drift accepted")
		}
	}
	got, err := compatibleDefectConfig(current, legacy, "wrong")
	if err == nil || got != nil {
		t.Fatal("invalid saved hash accepted")
	}
}
