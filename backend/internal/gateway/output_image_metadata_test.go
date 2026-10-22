package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

func encodedMetadataFixture(t *testing.T, media string) []byte {
	t.Helper()
	var b bytes.Buffer
	switch media {
	case "image/jpeg":
		if e := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 7, 11)), nil); e != nil {
			t.Fatal(e)
		}
	case "image/png":
		if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 7, 11))); e != nil {
			t.Fatal(e)
		}
	case "image/webp":
		b, e := os.ReadFile("testdata/intrinsic-rgb.webp")
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	return b.Bytes()
}
func TestIntrinsicImageDimensionsAreByteDerivedAndMimeBound(t *testing.T) {
	for _, media := range []string{"image/jpeg", "image/png", "image/webp"} {
		b := encodedMetadataFixture(t, media)
		m, e := decodeIntrinsicImageMetadata(media, b)
		if e != nil {
			t.Fatal(media, e)
		}
		w, h := 7, 11
		if media == "image/webp" {
			w, h = 513, 257
		}
		if m.Width != w || m.Height != h {
			t.Fatal(media, m)
		}
	}
	for _, x := range []struct {
		media string
		body  []byte
	}{{"image/jpeg", encodedMetadataFixture(t, "image/png")}, {"image/webp", []byte("not an image")}, {"image/png", []byte{137, 80, 78, 71}}} {
		if _, e := decodeIntrinsicImageMetadata(x.media, x.body); !errors.Is(e, objectstore.ErrIntegrity) {
			t.Fatal("invalid format committed", e)
		}
	}
}
func TestIntrinsicImagePixelBombAndEmptyDimensionRejectedWithoutPixelDecode(t *testing.T) {
	b := encodedMetadataFixture(t, "image/jpeg")
	at := bytes.Index(b, []byte{0xff, 0xc0})
	if at < 0 {
		t.Fatal("missing fixture SOF")
	}
	large := append([]byte(nil), b...)
	for _, offset := range []int{5, 6, 7, 8} {
		large[at+offset] = 255
	}
	if _, e := decodeIntrinsicImageMetadata("image/jpeg", large); !errors.Is(e, objectstore.ErrIntegrity) {
		t.Fatal("over200Mp admitted", e)
	}
	zero := append([]byte(nil), b...)
	zero[at+5], zero[at+6] = 0, 0
	if _, e := decodeIntrinsicImageMetadata("image/jpeg", zero); e == nil {
		t.Fatal("zero height admitted")
	}
}
func TestMetadataCaptureIsBoundedAndPreservesEntireImmutableWriterStream(t *testing.T) {
	body := bytes.Repeat([]byte{17}, imageMetadataHeaderLimit+12345)
	r, p := captureIntrinsicImageMetadata("image/jpeg", bytes.NewReader(body))
	h := sha256.New()
	n, e := io.Copy(h, r)
	want := sha256.Sum256(body)
	if e != nil || n != int64(len(body)) || !bytes.Equal(h.Sum(nil), want[:]) || len(p.data) != imageMetadataHeaderLimit {
		t.Fatal("capture truncated or retained full body", n, e, len(p.data))
	}
	r, p = captureIntrinsicImageMetadata("application/json", bytes.NewReader(body))
	if p != nil || r == nil {
		t.Fatal("nonimage behavior changed")
	}
}

type metadataRepository struct {
	outputRepository
	beginMetadata, completeMetadata *intrinsicImageMetadata
}

func (r *metadataRepository) Begin(ctx context.Context, q UploadRequest) (UploadTicket, error) {
	r.beginMetadata = q.intrinsicImage
	return r.outputRepository.Begin(ctx, q)
}
func (r *metadataRepository) Complete(ctx context.Context, q UploadRequest, t UploadTicket, o objectstore.Object) (ArtifactRef, error) {
	r.completeMetadata = q.intrinsicImage
	return r.outputRepository.Complete(ctx, q, t, o)
}
func metadataUploadFixture(t *testing.T, body []byte) (UploadRequest, *metadataRepository, *outputWriter) {
	_, id, _, _ := fixture(t)
	digest := sha256.Sum256(body)
	q := UploadRequest{Identity: id, Certificate: &x509.Certificate{Raw: []byte("verified-peer")}, IdempotencyKey: uuid.New(), Role: "derivative", MediaType: "image/jpeg", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(body)), intrinsicImage: &intrinsicImageMetadata{9999, 9999}}
	artifact := uuid.New()
	key, _ := objectstore.AttemptKey(id.TenantID, id.TaskID, artifact, id.Attempt, id.Fence)
	r := &metadataRepository{outputRepository: outputRepository{ticket: UploadTicket{ArtifactID: artifact, UploadID: uuid.New(), ObjectKey: key}}}
	return q, r, &outputWriter{}
}
func TestUploadIgnoresForgedMetadataAndCommitsMeasuredDimensions(t *testing.T) {
	body := encodedMetadataFixture(t, "image/jpeg")
	q, r, w := metadataUploadFixture(t, body)
	_, _, e := (&UploadService{Repository: r, Objects: w}).Upload(context.Background(), q, bytes.NewReader(body))
	if e != nil || r.beginMetadata != nil || r.completeMetadata == nil || *r.completeMetadata != (intrinsicImageMetadata{7, 11}) || r.completes != 1 {
		t.Fatal("not server-derived", e, r.completeMetadata)
	}
}
func TestMalformedImageNeverCommitsPublishedArtifact(t *testing.T) {
	body := []byte("fake image")
	q, r, w := metadataUploadFixture(t, body)
	_, _, e := (&UploadService{Repository: r, Objects: w}).Upload(context.Background(), q, bytes.NewReader(body))
	if !errors.Is(e, objectstore.ErrIntegrity) || r.completes != 0 || w.calls != 1 {
		t.Fatal("invalid image committed", e, r.completes)
	}
}
