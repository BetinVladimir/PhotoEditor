package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func analysisArrayFixture(t *testing.T, channels int, values []float32) (RelightAnalysisArraySpec, objectstore.Object, []byte) {
	t.Helper()
	shape := "(1, 1)"
	role := "WorkingAlpha"
	if channels > 0 {
		shape = fmt.Sprintf("(1, 1, %d)", channels)
		role = "WorkingPixels"
		if channels == 2 {
			role = "InverseField/0"
		}
	}
	header := []byte("{'descr': '<f4', 'fortran_order': False, 'shape': " + shape + ", }")
	for (10+len(header)+1)%64 != 0 {
		header = append(header, ' ')
	}
	header = append(header, '\n')
	raw := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(raw[8:10], uint16(len(header)))
	raw = append(raw, header...)
	for _, v := range values {
		var encoded [4]byte
		binary.LittleEndian.PutUint32(encoded[:], math.Float32bits(v))
		raw = append(raw, encoded[:]...)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	return RelightAnalysisArraySpec{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}, Role: role, Width: 1, Height: 1, Channels: channels, Alpha: channels == 0}, objectstore.Object{Key: "array", VersionID: "version1", ContentType: "application/x-npy", SHA256: digest, Bytes: int64(len(raw))}, raw
}
func TestAnalysisArraysCheckShapeFiniteAndAlphaWithoutClippingRGB(t *testing.T) {
	for _, sample := range []struct {
		channels int
		values   []float32
		valid    bool
	}{
		{3, []float32{-0.1, 0.5, 2}, true}, {2, []float32{-20, 200}, true}, {0, []float32{0.5}, true}, {0, []float32{2}, false}, {3, []float32{0, float32(math.NaN()), 1}, false}, {3, []float32{0, 1}, false},
	} {
		spec, object, raw := analysisArrayFixture(t, sample.channels, sample.values)
		err := VerifyRelightAnalysisArray(context.Background(), spec, object, bytes.NewReader(raw))
		if (err == nil) != sample.valid {
			t.Fatalf("channels %d validity=%v err=%v", sample.channels, sample.valid, err)
		}
	}
}
func TestAnalysisArrayRejectsChangedIdentityShapeAndObject(t *testing.T) {
	for _, fault := range []string{"width", "sha", "version", "media", "role", "truncated", "tail", "cancel"} {
		spec, object, raw := analysisArrayFixture(t, 3, []float32{0, 0.5, 1})
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "width":
			spec.Width = 2
		case "sha":
			object.SHA256 = "invalid"
		case "version":
			object.VersionID = "null"
		case "media":
			object.ContentType = "image/png"
		case "role":
			spec.Role = "untrusted"
		case "truncated":
			raw = raw[:len(raw)-1]
		case "tail":
			raw = append(raw, 0)
		case "cancel":
			cancel()
		}
		err := VerifyRelightAnalysisArray(ctx, spec, object, bytes.NewReader(raw))
		cancel()
		if err == nil {
			t.Fatal("accepted", fault)
		}
	}
}
func TestAnalysisSourceArraysUsePinnedSource(t *testing.T) {
	request := analysisPlanFixture(t)
	specs, err := AnalysisSourceArrays(request)
	if err != nil || len(specs) < 1 || specs[0].Role != "WorkingPixels" || specs[0].Channels != 3 {
		t.Fatal("invalid source specs", err)
	}
	request.SourceRaw = []byte("{}")
	if _, err := AnalysisSourceArrays(request); err == nil {
		t.Fatal("changed source accepted")
	}
}
