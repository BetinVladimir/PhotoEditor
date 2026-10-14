package interchange

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type importBinaryPublisherStub struct {
	calls int
	wrong bool
}

func (s *importBinaryPublisherStub) PublishImportBinary(_ context.Context, _ uuid.UUID, _ string, ref v2input.Ref, _ objectstore.Object) (v2input.Ref, error) {
	s.calls++
	if s.wrong {
		ref.SHA256 = rawSHA([]byte("foreign"))
	}
	return ref, nil
}
func (s *importBinaryPublisherStub) PublishImportLUT(_ context.Context, _ uuid.UUID, ref v2input.Ref, _ objectstore.Object, original, parent v2input.Ref, size int) (v2input.Ref, error) {
	if !validImportRef(original) || !validImportRef(parent) || (size != 33 && size != 65) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	return s.PublishImportBinary(context.Background(), uuid.Nil, "AbsoluteLUTArray", ref, objectstore.Object{})
}
func importMaskArrayFixture(shape string, values []float32) []byte {
	header := fmt.Sprintf("{'descr':'<f4','fortran_order':False,'shape':%s}\n", shape)
	raw := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(raw[8:], uint16(len(header)))
	raw = append(raw, header...)
	for _, value := range values {
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], math.Float32bits(value))
		raw = append(raw, word[:]...)
	}
	return raw
}
func TestImportMaskBinaryValidatesPhysicalNPYBeforePublication(t *testing.T) {
	for _, change := range []string{"none", "shape", "negative", "nan", "version", "receipt", "budget", "hash"} {
		t.Run(change, func(t *testing.T) {
			_, _, body := importWorkingValidatorFixture(t)
			image, err := review.ParseWorkingImage(body)
			if err != nil {
				t.Fatal(err)
			}
			image.Canvas.Width = 3
			image.Canvas.Height = 2
			steps := []any{}
			chainHash, err := canonical.Hash(map[string]any{"steps": steps})
			if err != nil {
				t.Fatal(err)
			}
			image.TransformChain, err = canonical.Marshal(map[string]any{"steps": steps, "hash": chainHash})
			if err != nil {
				t.Fatal(err)
			}
			master, err := canonical.Marshal(image)
			if err != nil {
				t.Fatal(err)
			}
			canvas, err := canonical.Hash(image.Canvas)
			if err != nil {
				t.Fatal(err)
			}
			target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(master)}, CanvasHash: canvas}
			old := target
			old.AssetVersionID = uuid.New()
			old.Source.ArtifactID = uuid.New()
			shape := "(2,3)"
			if change == "shape" {
				shape = "(3,2)"
			}
			values := []float32{0, .25, .5, .75, 1, 0}
			if change == "negative" {
				values[0] = -.1
			}
			if change == "nan" {
				values[0] = float32(math.NaN())
			}
			raw := importMaskArrayFixture(shape, values)
			sha := rawSHA(raw)
			archived := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
			staged := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
			objects := &importReaderStub{body: raw}
			if change == "hash" {
				objects.body = append([]byte(nil), raw...)
				objects.body[len(raw)-1] ^= 1
			}
			catalog := &importCatalogStub{object: objectstore.Object{Key: "mask", VersionID: "v1", ContentType: "application/octet-stream", SHA256: sha, Bytes: int64(len(raw))}, change: change == "version"}
			publisher := &importBinaryPublisherStub{wrong: change == "receipt"}
			masters := &importMasterReaderStub{body: master}
			remapper := ImportMaskBinaryRemapper{Workflow: uuid.New(), Catalog: catalog, Objects: objects, Masters: masters, Publisher: publisher}
			budget := int64(1 << 20)
			if change == "budget" {
				budget = int64(len(raw) - 1)
			}
			ref, size, err := remapper.RemapImportBinary(context.Background(), uuid.New(), archived, staged, "MaskPixels", old, target, budget)
			if change != "none" {
				if err == nil {
					t.Fatal("unqualified NPY accepted")
				}
				if change != "receipt" && publisher.calls != 0 {
					t.Fatal("publication before physical verification")
				}
				return
			}
			if err != nil || ref.ArtifactID == archived.ArtifactID || ref.SHA256 != sha || size != int64(len(raw)) || publisher.calls != 1 || catalog.calls != 3 || masters.calls != 2 {
				t.Fatal("physical remap or authority recheck missing", err)
			}
		})
	}
}
