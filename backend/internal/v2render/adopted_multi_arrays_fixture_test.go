package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"testing"
)

func adoptedArrayNPY(shape string, channels int, value float32) []byte {
	header := fmt.Sprintf("{'descr':'<f4','fortran_order':False,'shape':%s}\n", shape)
	raw := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(raw[8:], uint16(len(header)))
	raw = append(raw, header...)
	for i := 0; i < 100*100*channels; i++ {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(value))
		raw = append(raw, b[:]...)
	}
	return raw
}
func adoptedArraySHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Binds actual bytes all the way through source documents, Params and inventory.
// A corrupt but correctly SHA-bound body exercises semantic validation, not SHA alone.
func adoptedArrayFixture(t *testing.T, operation string, pixels, alpha []byte) (VerifiedMultiOriginals, uuid.UUID, *multiOriginalInventoryTestSource, *adoptedArrayObjects) {
	t.Helper()
	binding, task, inv, raw := originalInventoryFixture(t, operation)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["pixels"].(map[string]any)["sha256"] = adoptedArraySHA(pixels)
	doc["alpha"].(map[string]any)["sha256"] = adoptedArraySHA(alpha)
	raw, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	frames, _, calibration, err := binding.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	for i := range frames {
		frames[i].Source.SHA256 = adoptedArraySHA(raw)
		frames[i].CanvasHash = canvas
	}
	var params []byte
	if operation == "optical.panorama" {
		params, _, err = (v2optical.PanoramaParams{SchemaVersion: 1, Frames: frames, ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: calibration, Projection: "planar"}).Compile()
	} else {
		params, _, err = (v2optical.FocusStackParams{SchemaVersion: 1, Frames: frames, ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: calibration, MotionPolicy: "review"}).Compile()
	}
	if err != nil {
		t.Fatal(err)
	}
	binding.ParamsJCS = string(params)
	inv.binding = binding
	bodies := map[string][]byte{"original/pixels": pixels, "original/alpha": alpha}
	for i := range inv.items {
		item := &inv.items[i]
		body := raw
		if item.Role != "WorkingImage" {
			body = bodies[item.Object.Key]
		}
		item.Ref.SHA256 = adoptedArraySHA(body)
		item.Object.SHA256 = item.Ref.SHA256
		item.Object.Bytes = int64(len(body))
	}
	verified := VerifiedMultiOriginals{binding: binding, documents: []string{string(raw), string(raw)}, inventory: inv.owned()}
	return verified, task, &multiOriginalInventoryTestSource{inventory: inv}, &adoptedArrayObjects{bodies: bodies}
}

type adoptedArrayObjects struct {
	bodies         map[string][]byte
	opened, closed int
	before         func(objectstore.Object)
	closeErr       error
}

func (o *adoptedArrayObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.opened++
	if o.before != nil {
		o.before(obj)
	}
	body, ok := o.bodies[obj.Key]
	if !ok {
		return nil, errors.New("unexpected object")
	}
	return multiReadStream{Reader: bytes.NewReader(body), closed: &o.closed, closeErr: o.closeErr}, nil
}
func verifyAdoptedArrayFixture(ctx context.Context, v VerifiedMultiOriginals, task uuid.UUID, s *multiOriginalInventoryTestSource, o *adoptedArrayObjects) error {
	return VerifyAdoptedMultiOriginalArrays(ctx, s, o, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, v)
}
func TestAdoptedMultiArrayPhysicalBytesAndHDR(t *testing.T) {
	for _, op := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(op, func(t *testing.T) {
			v, task, s, o := adoptedArrayFixture(t, op, adoptedArrayNPY("(100,100,3)", 3, -2.5), adoptedArrayNPY("(100,100)", 1, .5))
			if err := verifyAdoptedArrayFixture(context.Background(), v, task, s, o); err != nil {
				t.Fatal(err)
			}
			if o.opened != 4 || o.closed != 4 || s.calls != 9 {
				t.Fatalf("occurrences or authority checks skipped: %d/%d/%d", o.opened, o.closed, s.calls)
			}
		})
	}
}
func TestAdoptedMultiArraySHABoundCorruptContent(t *testing.T) {
	for _, kind := range []string{"shape", "dtype", "nan", "alpha_range", "truncated", "sha"} {
		t.Run(kind, func(t *testing.T) {
			pixels := adoptedArrayNPY("(100,100,3)", 3, 2.5)
			alpha := adoptedArrayNPY("(100,100)", 1, .5)
			switch kind {
			case "shape":
				pixels = adoptedArrayNPY("(50,200,3)", 3, 2.5)
			case "dtype":
				pixels = bytes.Replace(pixels, []byte("<f4"), []byte(">f4"), 1)
			case "nan":
				pixels = adoptedArrayNPY("(100,100,3)", 3, float32(math.NaN()))
			case "alpha_range":
				alpha = adoptedArrayNPY("(100,100)", 1, 1.1)
			case "truncated":
				pixels = pixels[:len(pixels)-1]
			}
			v, task, s, o := adoptedArrayFixture(t, "optical.panorama", pixels, alpha)
			if kind == "sha" {
				o.bodies["original/pixels"] = bytes.Clone(pixels)
				o.bodies["original/pixels"][len(pixels)-1] ^= 1
			}
			if err := verifyAdoptedArrayFixture(context.Background(), v, task, s, o); !errors.Is(err, v2input.ErrInvalid) {
				t.Fatalf("%s: %v", kind, err)
			}
			if o.opened != o.closed || o.opened == 0 {
				t.Fatal("stream not closed")
			}
		})
	}
}
func TestAdoptedMultiArrayMutationAndIOFailures(t *testing.T) {
	for _, kind := range []string{"version", "cancel", "close"} {
		t.Run(kind, func(t *testing.T) {
			v, task, s, o := adoptedArrayFixture(t, "optical.focus_stack", adoptedArrayNPY("(100,100,3)", 3, 4), adoptedArrayNPY("(100,100)", 1, .5))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := error(v2input.ErrExpired)
			switch kind {
			case "version":
				o.before = func(objectstore.Object) { s.inventory.items[0].Object.VersionID = "replaced" }
			case "cancel":
				want = context.Canceled
				o.before = func(objectstore.Object) { cancel() }
			case "close":
				want = errors.New("close failed")
				o.closeErr = want
			}
			if err := verifyAdoptedArrayFixture(ctx, v, task, s, o); !errors.Is(err, want) {
				t.Fatalf("%s: %v", kind, err)
			}
			if o.opened != 1 || o.closed != 1 {
				t.Fatalf("cleanup %d/%d", o.opened, o.closed)
			}
		})
	}
}
