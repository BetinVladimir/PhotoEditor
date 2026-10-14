package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Byte-binding only; semantic construction and raster verification are separate.
func TestNativeManualParamsReplayRequiresOriginalCompilerBytes(t *testing.T) {
	body := `{"diameter_face_fraction":[0.003,0.008]}`
	sha := sha256.Sum256([]byte(body))
	compiled := PreparedDefectParams{params: body, digest: hex.EncodeToString(sha[:])}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: compiled.digest}
	if err := verifyNativeInputManualParamsReplay(context.Background(), compiled, ref, []byte(body)); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ref", "sha", "bytes", "compiled"} {
		pin, prepared, raw := ref, compiled, []byte(body)
		switch field {
		case "ref":
			pin.ArtifactID = uuid.Nil
		case "sha":
			pin.SHA256 = ""
		case "bytes":
			raw = append(raw, ' ')
		case "compiled":
			prepared.params = `{"diameter_face_fraction":[0.002,0.012]}`
		}
		if err := verifyNativeInputManualParamsReplay(context.Background(), prepared, pin, raw); err == nil {
			t.Fatal("changed Params accepted", field)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyNativeInputManualParamsReplay(ctx, compiled, ref, []byte(body)); err != context.Canceled {
		t.Fatal(err)
	}
}
