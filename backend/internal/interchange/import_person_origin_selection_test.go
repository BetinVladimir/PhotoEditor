package interchange

import (
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func importedPersonOriginSelectionFixture(t *testing.T) ([]byte, []byte,
	PortablePersonOriginExpectation, map[PortablePersonOriginKey]ed25519.PublicKey, v2input.Ref) {
	t.Helper()
	receiptRaw, expected, keys := portablePersonOriginFixture(t)
	document := importParsedValidatorFixture()
	document.Manifest.Entries[0].ID = expected.EntryID
	add := func(ref v2input.Ref, contract string) string {
		path := "dependencies/" + ref.ArtifactID.String() + ".json"
		document.Manifest.Inventory = append(document.Manifest.Inventory,
			importBundleInventory{ID: ref.ArtifactID, Path: path, SHA: ref.SHA256, Contract: contract})
		document.Files = append(document.Files, importParsedFile{Path: path,
			Content: v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}, Contract: contract})
		return path
	}
	recipePath := add(expected.Recipe, "EditingRecipeV3")
	recipeSHA := expected.Recipe.SHA256
	document.Manifest.Entries[0].RecipePath = &recipePath
	document.Manifest.Entries[0].RecipeSHA = &recipeSHA
	add(expected.Override, "Override")
	add(expected.MaskRef, "MaskRef")
	add(expected.MaskPixels, "MaskPixels")
	receipt := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(receiptRaw)}
	receiptPath := add(receipt, "PersonOriginReceipt")
	document.Manifest.PersonOrigins = []importBundlePersonOrigin{{EntryID: expected.EntryID,
		Recipe: expected.Recipe, Override: expected.Override, MaskRef: expected.MaskRef,
		MaskPixels: expected.MaskPixels, Path: receiptPath, SHA: receipt.SHA256}}
	raw, err := canonical.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return raw, receiptRaw, expected, keys, document.Files[len(document.Files)-1].Content
}

func TestImportedPersonOriginSelectsOnlySignedStagedOccurrence(t *testing.T) {
	parsed, raw, expected, keys, staged := importedPersonOriginSelectionFixture(t)
	selected, err := VerifyImportedPersonOriginFile(context.Background(), parsed, raw, expected, keys)
	if err != nil || selected.ReceiptFile != staged || selected.Claim.EntryID != expected.EntryID {
		t.Fatalf("valid staged receipt refused: %v", err)
	}
	badScope := expected
	badScope.EntryID = uuid.New()
	if _, err := VerifyImportedPersonOriginFile(context.Background(), parsed, raw, badScope, keys); err == nil {
		t.Fatal("foreign entry admitted")
	}
	if _, err := VerifyImportedPersonOriginFile(context.Background(), parsed, append(raw, ' '), expected, keys); err == nil {
		t.Fatal("changed receipt bytes admitted")
	}
	if _, err := VerifyImportedPersonOriginFile(context.Background(), parsed, raw, expected, nil); err == nil {
		t.Fatal("absent process trust admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyImportedPersonOriginFile(ctx, parsed, raw, expected, keys); err != context.Canceled {
		t.Fatalf("cancelled selection continued: %v", err)
	}
}
