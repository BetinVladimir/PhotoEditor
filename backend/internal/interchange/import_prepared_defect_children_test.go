package interchange

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPreparedDefectDocumentsProjectTypedChildren(t *testing.T) {
	for _, tc := range []struct {
		kind string
		file string
		want map[string]bool
	}{
		{"DefectCandidates", "../review/testdata/defect-proposal-python.json", map[string]bool{"WorkingImage": false, "WorkingPixels": false, "DefectLabelMap": false}},
		{"DefectReview", "../review/testdata/defect-selection-python.json", map[string]bool{"WorkingImage": false, "DefectCandidates": false, "DefectLabelMap": false}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			raw, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			entry := v2input.Entry{Contract: tc.kind, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}}
			children, err := (ImportPreparedSchemaChildren{}).ImportDocumentChildren(context.Background(), entry, raw)
			if err != nil || len(children) != len(tc.want) {
				t.Fatalf("typed child projection: %v %v", children, err)
			}
			for _, child := range children {
				if _, ok := tc.want[child.Contract]; !ok || tc.want[child.Contract] {
					t.Fatalf("unexpected or duplicate child role: %v", child)
				}
				tc.want[child.Contract] = true
			}
			for role, seen := range tc.want {
				if !seen {
					t.Fatalf("missing role %s", role)
				}
			}
		})
	}
}
