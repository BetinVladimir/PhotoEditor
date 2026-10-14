package interchange

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestImportPersonMaskConsentChainRechecksCurrentDecision(t *testing.T) {
	denied := errors.New("consent revoked")
	for _, mode := range []string{"valid", "current_before", "other", "current_after", "direct_mask"} {
		t.Run(mode, func(t *testing.T) {
			calls := []string{}
			current := preparedInstallationMaskQualifier(func(context.Context, review.MaskRef, v2input.SessionVersionBinding) error {
				calls = append(calls, "current")
				if mode == "current_before" || mode == "current_after" && len(calls) == 3 {
					return denied
				}
				return nil
			})
			other := preparedInstallationMaskQualifier(func(context.Context, review.MaskRef, v2input.SessionVersionBinding) error {
				calls = append(calls, "other")
				if mode == "other" {
					return denied
				}
				return nil
			})
			mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA([]byte("mask"))}}
			ctx := context.Background()
			if mode != "direct_mask" {
				ctx = withPreparedOverrideMaskScope(ctx, v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("override"))}, v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256})
			}
			err := (importPersonMaskConsentChain{current: current, other: other}).QualifyImportedMask(ctx, mask, v2input.SessionVersionBinding{})
			if mode == "direct_mask" {
				if err == nil || len(calls) != 0 {
					t.Fatal("direct person mask inherited Override consent", err, calls)
				}
				return
			}
			if mode == "valid" {
				if err != nil || !reflect.DeepEqual(calls, []string{"current", "other", "current"}) {
					t.Fatal("current decision not repeated", err, calls)
				}
				return
			}
			if !errors.Is(err, denied) {
				t.Fatal("consent refusal lost", err)
			}
			if mode == "current_before" && !reflect.DeepEqual(calls, []string{"current"}) {
				t.Fatal("external authority ran after current revocation", calls)
			}
		})
	}
}
