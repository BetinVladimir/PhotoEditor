package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type importOverrideContentStub struct {
	calls, failAt int
	failure       error
}

func (s *importOverrideContentStub) QualifyImportOverrideContent(_ context.Context, body []byte, _ v2input.SessionVersionBinding) error {
	s.calls++
	body[0] = 'x'
	if s.calls == s.failAt {
		return s.failure
	}
	return nil
}
func TestPreparedOverrideChecksSourceMasksAndFullContent(t *testing.T) {
	for _, change := range []string{"none", "content", "content_revoked", "mask", "source", "candidate", "confirmation", "confirmation_authorized", "confirmation_revoked", "missing", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			_, _, master := importWorkingValidatorFixture(t)
			image, err := review.ParseWorkingImage(master)
			if err != nil {
				t.Fatal(err)
			}
			canvas, _ := canonical.Hash(image.Canvas)
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(master)}, CanvasHash: canvas}
			mask := map[string]any{"artifact": v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("pixels"))}, "canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain), "object_id":nil,"model_lock_id":nil,"selector": "manual", "confidence": 1, "protected": true}
			inline := map[string]any{"binding_id": "guard-1", "purpose": "protected", "mask": mask, "strength": .5}
			if change == "confirmation" || change == "confirmation_authorized" || change == "confirmation_revoked" {
				inline["confirmed"] = true
			}
			if change == "confirmation_authorized" || change == "confirmation_revoked" {
				mask["selector"] = "person"
				mask["object_id"] = nil
				mask["model_lock_id"] = rawSHA([]byte("model"))
			}
			document := map[string]any{"schema_version": 1, "input_sha256": image.Canvas.SourceSHA256, "modules": map[string]any{"strength": .5}, "mask_refs": []any{inline}, "revision": 1}
			if change == "candidate" {
				document["accepted_candidate_ids"] = []string{"foreign"}
			}
			if change == "source" {
				document["input_sha256"] = rawSHA([]byte("foreign source"))
			}
			raw, err := canonical.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("qualification unavailable")
			content := &importOverrideContentStub{failure: failure}
			if change == "content" {
				content.failAt = 1
			}
			if change == "content_revoked" {
				content.failAt = 2
			}
			masks := &importMaskQualifierStub{}
			if change == "mask" {
				masks.failure = failure
			}
			masters := &importMasterReaderStub{body: master}
			q := ImportPreparedOverrideQualifier{Masters: masters, Masks: masks, Content: content}
			if change == "missing" {
				q.Content = nil
			}
			authority := &confirmedMaskAuthorityStub{}
			if change == "confirmation_authorized" || change == "confirmation_revoked" {
				q.Confirmed = authority
			}
			if change == "confirmation_revoked" {
				authority.failure = failure
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if change == "cancelled" {
				cancel()
			}
			err = q.QualifyImportedOverride(ctx, raw, binding)
			if change == "none" || change == "confirmation_authorized" {
				if err != nil || content.calls != 2 || masks.calls != 1 || masters.calls != 2 || raw[0] != '{' {
					t.Fatal("qualification/ownership", err)
				}
				if change == "confirmation_authorized" && authority.preparedCalls != 3 {
					t.Fatal("confirmation not repeated after IO")
				}
				return
			}
			if err == nil {
				t.Fatal("incomplete qualification accepted")
			}
			if (change == "content" || change == "content_revoked" || change == "mask" || change == "confirmation_revoked") && !errors.Is(err, failure) {
				t.Fatal("failure lost", err)
			}
			if change == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if (change == "candidate" || change == "confirmation" || change == "confirmation_revoked" || change == "content" || change == "source" || change == "missing" || change == "cancelled") && masks.calls != 0 {
				t.Fatal("rejected override reached physical IO")
			}
		})
	}
}
