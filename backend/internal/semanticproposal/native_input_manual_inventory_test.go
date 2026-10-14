package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

// Pin-selection test only; the private shape fixture does not prove raster IO.
func TestNativeManualInventoryPinsBindOriginalFiles(t *testing.T) {
	proof := manualPinTestProof()
	body := []byte(`{"root":true}`)
	sha := sha256.Sum256(body)
	proof.objects.Root.ref.SHA256 = hex.EncodeToString(sha[:])
	proof.objects.Root.object.SHA256 = proof.objects.Root.ref.SHA256
	proof.objects.Root.object.Bytes = int64(len(body))
	original := review.OriginalManualProtection{Root: review.ArtifactRef{ID: proof.objects.Root.ref.ArtifactID, SHA256: proof.objects.Root.ref.SHA256}, Body: body, Target: review.ProtectionMask{Artifact: review.ArtifactRef{ID: proof.objects.Pixel.ref.ArtifactID, SHA256: proof.objects.Pixel.ref.SHA256}}}
	selection := nativeInputManualSelection{manuals: []review.OriginalManualProtection{original}, proofs: []defectManualExecutionProof{ownDefectManualExecutionProof(proof)}}
	pins, err := nativeInputManualInventoryPins(context.Background(), selection, "original-bucket")
	if err != nil || len(pins) != 3 {
		t.Fatal("incomplete worker-declared files", err)
	}
	for _, pin := range pins {
		if pin.pin.ref == proof.objects.Params.ref || pin.pin.ref == proof.children[1].ref || pin.pin.ref == proof.children[2].ref {
			t.Fatal("undeclared evidence added as worker download")
		}
	}
	selection.manuals[0].Target.Artifact.ID = uuid.New()
	if pins, err := nativeInputManualInventoryPins(context.Background(), selection, "original-bucket"); err == nil || pins != nil {
		t.Fatal("foreign manual target accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if pins, err := nativeInputManualInventoryPins(ctx, selection, "original-bucket"); err != context.Canceled || pins != nil {
		t.Fatal("cancelled inventory exposed pins", err)
	}
}
