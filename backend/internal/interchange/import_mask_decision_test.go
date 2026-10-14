package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestImportPersonMaskDecisionRejectsIncompleteGrantAndForeignRevokePayload(t *testing.T) {
	base := ImportPersonMaskDecisionRequest{
		Action:             "revoke",
		EntryID:            uuid.New(),
		TargetVersionID:    uuid.New(),
		OverrideArtifactID: uuid.New(),
		MaskArtifactID:     uuid.New(),
	}
	if err := validateImportPersonMaskDecision(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"grant_without_evidence", "revoke_with_archive", "missing_target", "unknown_action", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			request := base
			ctx := context.Background()
			switch change {
			case "grant_without_evidence":
				request.Action = "grant"
			case "revoke_with_archive":
				request.ParsedJCS = `{}`
			case "missing_target":
				request.TargetVersionID = uuid.Nil
			case "unknown_action":
				request.Action = "archive_confirmed"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := validateImportPersonMaskDecision(ctx, request); err == nil {
				t.Fatal("invalid mask decision accepted")
			}
		})
	}
}
