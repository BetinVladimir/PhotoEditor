package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestNativeManualSavedProtectionBindsExactDocumentBytesAndResult(t *testing.T) {
	prepared := nativeInputManualInitialProtectionPreparation{document: uuid.New(), body: `{"protected":true}`, result: completedNativeInputDefect{catalog: nativeInputDefectResultCatalog{ResultID: uuid.New()}}}
	sha := sha256.Sum256([]byte(prepared.body))
	saved := nativeInputManualSavedProtection{root: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sha[:])}, document: prepared.document, result: prepared.result.catalog.ResultID}
	if err := verifyNativeInputManualSavedProtection(saved, prepared); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"root", "sha", "document", "result", "body"} {
		got, proof := saved, prepared
		switch field {
		case "root":
			got.root.ArtifactID = uuid.Nil
		case "sha":
			got.root.SHA256 = ""
		case "document":
			got.document = uuid.New()
		case "result":
			got.result = uuid.New()
		case "body":
			proof.body += " "
		}
		if err := verifyNativeInputManualSavedProtection(got, proof); err == nil {
			t.Fatal("changed saved protection accepted", field)
		}
	}
}

func TestNativeManualSavedProtectionRestoreCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	saved, err := reader.readNativeInputManualSavedProtection(ctx, auth.Identity{}, nativeInputManualInitialProtectionPreparation{})
	if !errors.Is(err, context.Canceled) || saved.root.ArtifactID != uuid.Nil {
		t.Fatal("cancelled locator exposed root", err)
	}
	registered, err := reader.restoreRegisteredNativeInputManualInitialProtection(ctx, auth.Identity{}, nativeInputManualInitialDraftPlan{}, TransferColorProfile{})
	if !errors.Is(err, context.Canceled) || registered.verified.Root.ArtifactID != uuid.Nil {
		t.Fatal("cancelled restore exposed registered protection", err)
	}
}
