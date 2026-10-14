package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"strings"
	"testing"
)

func TestRenderSubmissionDecodeExactPublicIntent(t *testing.T) {
	sha := strings.Repeat("a", 64)
	intent := RenderBatchSubmissionRequest{SnapshotID: uuid.New(), PolicySHA: sha, Selection: []RenderSelectionItem{{Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, CanvasHash: sha}, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}}}}
	raw, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeRenderBatchSubmissionRequest(raw)
	if err != nil || !reflect.DeepEqual(out, intent) {
		t.Fatal("valid intent altered", err)
	}
	body := string(raw)
	for name, invalid := range map[string]string{
		"case alias":               strings.Replace(body, `"input_snapshot_id":`, `"INPUT_SNAPSHOT_ID":`, 1),
		"duplicate exact key":      strings.Replace(body, `"policy_sha":`, `"policy_sha":"`+sha+`","policy_sha":`, 1),
		"duplicate alias":          strings.Replace(body, `"policy_sha":`, `"POLICY_SHA":"`+sha+`","policy_sha":`, 1),
		"null nested ref":          strings.Replace(body, `"source":{"artifact_id":"`+intent.Selection[0].Binding.Source.ArtifactID.String()+`","sha256":"`+sha+`"}`, `"source":null`, 1),
		"null recipe":              strings.Replace(body, `"recipe":{"artifact_id":"`+intent.Selection[0].Recipe.ArtifactID.String()+`","sha256":"`+sha+`"}`, `"recipe":null`, 1),
		"unknown execution choice": strings.Replace(body, `"selection":`, `"runtime_sha":"`+sha+`","selection":`, 1),
		"trailing value":           body + ` {}`,
		"oversize":                 strings.Repeat(" ", MaxRenderSubmissionBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := DecodeRenderBatchSubmissionRequest([]byte(invalid)); err == nil || out.SnapshotID != uuid.Nil {
				t.Fatal("malformed intent accepted", err)
			}
		})
	}
}
