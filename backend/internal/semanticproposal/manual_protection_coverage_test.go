package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func manualCoverageNPY(values []float32) []byte {
	header := "{'descr': '<f4', 'fortran_order': False, 'shape': (2, 3), }"
	header += strings.Repeat(" ", (16-(10+len(header)+1)%16)%16) + "\n"
	out := append([]byte{'\x93', 'N', 'U', 'M', 'P', 'Y', 1, 0}, byte(len(header)), byte(len(header)>>8))
	out = append(out, []byte(header)...)
	for _, v := range values {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	return out
}

func TestManualProtectionCoverageReadsPinnedProbabilityPixels(t *testing.T) {
	required := manualCoverageNPY([]float32{0, .25, .5, 1, 0, .8})
	for _, mode := range []string{"covered", "weakened", "sha", "close"} {
		t.Run(mode, func(t *testing.T) {
			values := []float32{1, .25, .5, 1, 0, .8}
			if mode == "weakened" {
				values[1] = .249
			}
			protected := manualCoverageNPY(values)
			store := &transferReadStore{bodies: map[string][]byte{"required": required, "protected": protected}}
			makePin := func(key string, body []byte) defectEvidenceObject {
				hash := sha256.Sum256(body)
				digest := hex.EncodeToString(hash[:])
				return defectEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}, object: objectstore.Object{Key: key, VersionID: "original-version", Bytes: int64(len(body)), SHA256: digest, ContentType: "application/octet-stream"}}
			}
			left, right := makePin("required", required), makePin("protected", protected)
			if mode == "sha" {
				right.ref.SHA256 = strings.Repeat("a", 64)
			}
			closeFailure := errors.New("stream close failed")
			if mode == "close" {
				store.closeErr = closeFailure
			}
			err := (SQLTransferSourceReader{Objects: store}).verifyManualProtectionCoverage(context.Background(), left, right, 3, 2)
			if mode == "covered" && err != nil {
				t.Fatal(err)
			}
			if mode != "covered" && err == nil {
				t.Fatal("invalid probability coverage accepted", mode)
			}
			if mode == "close" && !errors.Is(err, closeFailure) {
				t.Fatal(err)
			}
			if len(store.opened) != 2 || store.closed != 2 || store.opened[0] != left.object || store.opened[1] != right.object {
				t.Fatal("original version IO/close mismatch", store.opened, store.closed)
			}
		})
	}
}
