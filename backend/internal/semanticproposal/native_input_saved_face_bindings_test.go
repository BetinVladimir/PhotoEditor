package semanticproposal

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
)

func TestSavedNativeFaceBindingsRejectChangedOriginalIdentity(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Params struct {
			Faces []review.DefectProposalFaceInput `json:"faces"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &document) != nil || len(document.Params.Faces) == 0 {
		t.Fatal("fixture")
	}
	actual := document.Params.Faces
	expected := append([]review.DefectProposalFaceInput(nil), actual...)
	for i := range expected {
		expected[i].Manual = nil
	}
	if err := verifyNativeInputSavedFaceBindings(context.Background(), actual, expected); err != nil {
		t.Fatal(err)
	}
	changed := append([]review.DefectProposalFaceInput(nil), actual...)
	changed[0].Landmarks.ID = uuid.New()
	if err := verifyNativeInputSavedFaceBindings(context.Background(), changed, expected); err == nil {
		t.Fatal("foreign landmarks accepted")
	}
	if err := verifyNativeInputSavedFaceBindings(context.Background(), actual[:len(actual)-1], expected); err == nil {
		t.Fatal("missing face accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyNativeInputSavedFaceBindings(ctx, actual, expected); err != context.Canceled {
		t.Fatal(err)
	}
	selection, err := (SQLTransferSourceReader{}).readNativeInputSavedManualInputs(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{})
	if err != context.Canceled || selection.manuals != nil || selection.proofs != nil {
		t.Fatal("cancelled native manual preflight exposed inputs", err)
	}
}
