package semanticproposal

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestNativeSkinDerivationPhysicalBytes(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/face-labels19-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var labels review.FaceLabels19
	if err := json.Unmarshal(raw, &labels); err != nil {
		t.Fatal(err)
	}
	w, h := int(labels.Canvas.Width), int(labels.Canvas.Height)
	npy := func(dtype string, width int, first uint32) []byte {
		header := fmt.Sprintf("{'descr': '%s', 'fortran_order': False, 'shape': (%d, %d), }", dtype, h, w)
		header += strings.Repeat(" ", (64-(10+len(header)+1)%64)%64) + "\n"
		body := append([]byte("\x93NUMPY\x01\x00"), byte(len(header)), byte(len(header)>>8))
		body = append(body, header...)
		payload := make([]byte, w*h*width)
		if width == 2 {
			binary.LittleEndian.PutUint16(payload, uint16(first))
		} else {
			binary.LittleEndian.PutUint32(payload, first)
		}
		return append(body, payload...)
	}
	store := &transferReadStore{bodies: map[string][]byte{}}
	pin := func(key, media string, body []byte) defectEvidenceObject {
		sum := sha256.Sum256(body)
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		store.bodies[key] = body
		return defectEvidenceObject{ref: ref, object: objectstore.Object{Key: key, VersionID: "immutable", SHA256: ref.SHA256, Bytes: int64(len(body)), ContentType: media}}
	}
	labelPin := pin("labels", "application/octet-stream", npy("<u2", 2, 1))
	labels.Labels = review.ArtifactRef{ID: labelPin.ref.ArtifactID, SHA256: labelPin.ref.SHA256}
	labels.Faces.ID = uuid.New()
	manifestBody, err := canonical.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	manifest := pin("manifest", "application/json", manifestBody)
	paramsBody, err := canonical.Marshal(map[string]any{"labels": manifest.ref, "algorithm": "bisenet_face19_skin_v1"})
	if err != nil {
		t.Fatal(err)
	}
	params := pin("params", "application/json", paramsBody)
	pixels := pin("mask", "application/octet-stream", npy("<f4", 4, math.Float32bits(1)))
	var encoded bytes.Buffer
	encoded.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	chunk := func(kind string, body []byte) {
		binary.Write(&encoded, binary.BigEndian, uint32(len(body)))
		encoded.WriteString(kind)
		encoded.Write(body)
		binary.Write(&encoded, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(kind), body...)))
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[:4], uint32(w))
	binary.BigEndian.PutUint32(header[4:8], uint32(h))
	header[8] = 16
	chunk("IHDR", header)
	raster := make([]byte, h*(1+2*w))
	raster[1] = 255
	raster[2] = 255
	var compressed bytes.Buffer
	zipper := zlib.NewWriter(&compressed)
	if _, err := zipper.Write(raster); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	chunk("IDAT", compressed.Bytes())
	chunk("IEND", nil)
	pngPin := pin("png", "image/png", encoded.Bytes())
	mask := review.ProtectionMask{Artifact: review.ArtifactRef{ID: pixels.ref.ArtifactID, SHA256: pixels.ref.SHA256}, Canvas: labels.Canvas, TransformChain: labels.TransformChain, Selector: "skin", Confidence: labels.MeanConfidence, ModelLockID: &labels.ModelLockID}
	source := review.DefectSource{Image: labels.Image, Canvas: labels.Canvas, TransformChain: labels.TransformChain}
	reader := SQLTransferSourceReader{Objects: store}
	if _, err := masknpy.VerifyFaceSkinNPY(context.Background(), masknpy.PinnedMask{Reader: bytes.NewReader(store.bodies["labels"]), Bytes: labelPin.object.Bytes, SHA256: labelPin.ref.SHA256}, masknpy.PinnedMask{Reader: bytes.NewReader(store.bodies["mask"]), Bytes: pixels.object.Bytes, SHA256: pixels.ref.SHA256}, int64(w), int64(h)); err != nil {
		t.Fatal("NPY derivation", err)
	}
	if err := reader.verifyProbabilityPNG(context.Background(), pixels, pngPin, int64(w), int64(h)); err != nil {
		t.Fatal("PNG consistency", err)
	}
	count, err := reader.verifyNativeSkinDerivation(context.Background(), params.ref, paramsBody, manifest.ref, manifestBody, mask, source, labelPin, pixels, pngPin)
	if err != nil || count != 1 {
		t.Fatal("valid physical skin derivation", err, count)
	}
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"fractional", npy("<f4", 4, math.Float32bits(.5))},
		{"nan", npy("<f4", 4, math.Float32bits(float32(math.NaN())))},
		{"infinity", npy("<f4", 4, math.Float32bits(float32(math.Inf(1))))},
		{"wrong-class", npy("<f4", 4, 0)},
		{"truncated", store.bodies["mask"][:len(store.bodies["mask"])-1]},
		{"trailing-byte", append(append([]byte(nil), store.bodies["mask"]...), 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := pin("wrong-mask-"+tc.name, "application/octet-stream", tc.body)
			candidate := mask
			candidate.Artifact = review.ArtifactRef{ID: bad.ref.ArtifactID, SHA256: bad.ref.SHA256}
			count, err := reader.verifyNativeSkinDerivation(context.Background(), params.ref, paramsBody, manifest.ref, manifestBody, candidate, source, labelPin, bad, pngPin)
			if err == nil || count != 0 {
				t.Fatal("invalid physical mask accepted", err, count)
			}
			if store.closed != len(store.opened) {
				t.Fatal("verification leaked streams")
			}
		})
	}
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"png-crc", append([]byte(nil), encoded.Bytes()...)},
		{"png-truncated", encoded.Bytes()[:encoded.Len()-1]},
		{"png-trailing", append(append([]byte(nil), encoded.Bytes()...), 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "png-crc" {
				tc.body[len(tc.body)-1] ^= 1
			}
			bad := pin(tc.name, "image/png", tc.body)
			count, err := reader.verifyNativeSkinDerivation(context.Background(), params.ref, paramsBody, manifest.ref, manifestBody, mask, source, labelPin, pixels, bad)
			if err == nil || count != 0 {
				t.Fatal("invalid PNG accepted", err, count)
			}
			if store.closed != len(store.opened) {
				t.Fatal("verification leaked streams")
			}
		})
	}
	if store.closed != len(store.opened) {
		t.Fatal("skin verification leaked streams")
	}
}
