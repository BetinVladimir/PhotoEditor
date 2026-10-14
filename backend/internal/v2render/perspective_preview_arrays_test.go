package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
 "strings"
 "github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type previewArrayFixture struct {
	*previewBundleFixture
	lookups int
	replace bool
}

func (f *previewArrayFixture) ResolveArray(ctx context.Context, pin PerspectivePreviewSource, node v2draft.LayerRef, _ []byte) (objectstore.Object, error) {
	f.lookups++
	obj, err := f.ResolveObject(ctx, pin, node)
	obj.ContentType = "application/x-npy"
	if f.replace && f.lookups == 2 {
		obj.VersionID = "replaced"
	}
	return obj, err
}
func previewArrayBytes(width, height int, alpha bool, value float32) []byte {
	shape := fmt.Sprintf("(%d, %d, 3)", height, width)
	count := width * height * 3
	if alpha {
		shape = fmt.Sprintf("(%d, %d)", height, width)
		count = width * height
	}
	header := []byte("{'descr': '<f4', 'fortran_order': False, 'shape': " + shape + ", }")
	for (10+len(header)+1)%64 != 0 {
		header = append(header, ' ')
	}
	header = append(header, '\n')
	raw := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(raw[8:10], uint16(len(header)))
	raw = append(raw, header...)
	for i := 0; i < count; i++ {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(value))
		raw = append(raw, b[:]...)
	}
	return raw
}
func previewArrayBundle(t *testing.T, rgb, alpha float32) (*previewArrayFixture, PerspectivePreviewBundle) {
	t.Helper()
	f := &previewArrayFixture{previewBundleFixture: newPreviewBundleFixture(t)}
	root, err := v2draft.ParseImageResult(f.docs[f.pin.Result.ArtifactID.String()], f.pin.Result)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(f.docs[root.WorkingImage.ArtifactID.String()])
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		ref   *review.ArtifactRef
		alpha bool
		value float32
	}{{&image.Pixels, false, rgb}, {image.Alpha, true, alpha}} {
		raw := previewArrayBytes(int(image.Canvas.Width), int(image.Canvas.Height), entry.alpha, entry.value)
		entry.ref.SHA256 = renderPreviewArraySHA(raw)
		f.docs[entry.ref.ID.String()] = raw
	}
	f.docs[root.WorkingImage.ArtifactID.String()], err = canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	root.WorkingImage.SHA256 = renderPreviewArraySHA(f.docs[root.WorkingImage.ArtifactID.String()])
	f.docs[f.pin.Result.ArtifactID.String()], err = canonical.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	f.pin.Result.SHA256 = renderPreviewArraySHA(f.docs[f.pin.Result.ArtifactID.String()])
	bundle, err := LoadPerspectivePreviewBundle(context.Background(), f, f, f, f.pin.Tenant, f.pin.Producer)
	if err != nil {
		t.Fatal(err)
	}
	return f, bundle
}
func TestPerspectivePreviewArraysStreamOriginalContent(t *testing.T) {
	for _, sample := range []struct {
		rgb, alpha float32
		valid      bool
	}{{-1, .5, true}, {2, 1, true}, {0, 2, false}, {float32(math.NaN()), .5, false}} {
		f, bundle := previewArrayBundle(t, sample.rgb, sample.alpha)
		verified, err := VerifyPerspectivePreviewInputs(context.Background(), f, f, f, bundle)
		if (err == nil) != sample.valid {
			t.Fatal("array invariant", sample, err)
		}
		if sample.valid {
			if len(verified.Arrays()) != 2 || verified.Source() != f.pin {
				t.Fatal("missing witnesses")
			}
			owned := verified.Arrays()
			owned[0].Object.VersionID = "changed"
			if verified.Arrays()[0].Object.VersionID == "changed" {
				t.Fatal("borrowed witness")
			}
		}
	}
}
func TestPerspectivePreviewArraysRefuseReplacedLocatorAndCancellation(t *testing.T) {
	f, bundle := previewArrayBundle(t, 0, .5)
	f.replace = true
	if _, err := VerifyPerspectivePreviewInputs(context.Background(), f, f, f, bundle); err != v2input.ErrExpired {
		t.Fatal("replacement accepted", err)
	}
	f, bundle = previewArrayBundle(t, 0, .5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyPerspectivePreviewInputs(ctx, f, f, f, bundle); err != context.Canceled || f.lookups != 0 {
		t.Fatal("cancelled verification reached IO", err)
	}
}

func renderPreviewArraySHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// A replacement receipt identity is part of the final observation even when
// immutable array objects themselves have not changed.
type changedReplacementArraySource struct { pin PerspectivePreviewSource }
func (s changedReplacementArraySource) Resolve(context.Context, uuid.UUID, uuid.UUID) (PerspectivePreviewSource,error) {
 pin:=s.pin
 pin.Replacement.DecisionHash=strings.Repeat("e",64)
 return pin,nil
}
func TestPerspectivePreviewReplacementArraysRetainReceiptIdentity(t *testing.T) {
 for _,changed:=range []bool{false,true} {
  f,bundle:=previewArrayBundle(t,2,.5)
  id:=PerspectivePreviewReplacementIdentity{Review:uuid.New(),OriginalTask:uuid.New(),OriginalRun:uuid.New(),DecisionHash:strings.Repeat("d",64)}
  pin:=f.pin
  namespace:=uuid.NewSHA1(pin.Tenant,[]byte("V29.NegativeReplacement/v2:"+pin.Project.String()+":"+id.Review.String()+":"+id.DecisionHash))
  pin.Run=uuid.NewSHA1(namespace,[]byte("run:"+id.OriginalRun.String()+":"+id.OriginalTask.String()))
  var err error
  pin.Workflow,err=v2input.WorkflowID(pin.Run);if err!=nil{t.Fatal(err)}
  pin.Producer=uuid.NewSHA1(namespace,[]byte("task:"+id.OriginalTask.String()))
  pin.Replacement=id
  f.pin=pin;bundle.pin=pin
  var sources PerspectivePreviewSourceReader=f
  if changed{sources=changedReplacementArraySource{pin:pin}}
  verified,err:=VerifyPerspectivePreviewInputs(context.Background(),sources,f,f,bundle)
  if changed {
   if err!=v2input.ErrExpired{t.Fatal("changed receipt identity accepted",err)}
  } else {
   if err!=nil{t.Fatal(err)}
   if verified.Source()!=pin||len(verified.Arrays())!=2{t.Fatal("replacement proof lost")}
   exposed:=verified.Source();exposed.Replacement.DecisionHash=strings.Repeat("f",64)
   if verified.Source()!=pin{t.Fatal("replacement proof aliases caller")}
  }
 }
}
