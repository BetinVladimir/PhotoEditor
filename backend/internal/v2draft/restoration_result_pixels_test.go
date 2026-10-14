package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

type restorationArrayCatalogFixture struct {
	objects map[string]objectstore.Object
	reads   map[string]int
	change  bool
}

func (f *restorationArrayCatalogFixture) ResolveRestorationArray(_ context.Context, _ RestorationArrayScope,
	role string, ref v2input.Ref) (objectstore.Object, error) {
	object, ok := f.objects[role]
	if !ok || object.SHA256 != ref.SHA256 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	f.reads[role]++
	if f.change && f.reads[role] > 1 && role == "output_pixels" {
		object.VersionID = "changed"
	}
	return object, nil
}

type restorationArrayReaderFixture map[string][]byte

func (f restorationArrayReaderFixture) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f[object.Key])), nil
}

func restorationArrayFixture(shape string, values []float32) []byte {
	header := []byte(fmt.Sprintf("{'descr': '<f4', 'fortran_order': False, 'shape': %s}\n", shape))
	raw := make([]byte, 10+len(header)+len(values)*4)
	copy(raw, []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0})
	binary.LittleEndian.PutUint16(raw[8:10], uint16(len(header)))
	copy(raw[10:], header)
	for i, value := range values {
		binary.LittleEndian.PutUint32(raw[10+len(header)+i*4:], math.Float32bits(value))
	}
	return raw
}

func TestRestorationResultPixelStreamsBindDocuments(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	source, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	width, height := source.Canvas.Width, source.Canvas.Height
	pixelCount := int(width * height)
	sourceValues := make([]float32, pixelCount*3)
	outputValues := make([]float32, pixelCount*3)
	outputValues[3] = .25
	targetValues, protectedValues, changedValues := make([]float32, pixelCount), make([]float32, pixelCount), make([]float32, pixelCount)
	targetValues[1], changedValues[1] = 1, 1
	shapeRGB := fmt.Sprintf("(%d,%d,3)", height, width)
	shapeMask := fmt.Sprintf("(%d,%d)", height, width)
	sourceBytes := restorationArrayFixture(shapeRGB, sourceValues)
	outputBytes := restorationArrayFixture(shapeRGB, outputValues)
	targetBytes := restorationArrayFixture(shapeMask, targetValues)
	protectedBytes := restorationArrayFixture(shapeMask, protectedValues)
	changedBytes := restorationArrayFixture(shapeMask, changedValues)
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	artifact := func(value v2input.Ref) review.ArtifactRef {
		return review.ArtifactRef{ID: value.ArtifactID, SHA256: value.SHA256}
	}
	marshal := func(value any) []byte {
		raw, e := canonical.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	sourcePixelRef, outputPixelRef := ref(sourceBytes), ref(outputBytes)
	targetPixelRef, protectedPixelRef, changedPixelRef := ref(targetBytes), ref(protectedBytes), ref(changedBytes)
	source.Pixels = artifact(sourcePixelRef)
	sourceRaw := marshal(source)
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(sourceRaw)}
	base.CanvasHash, err = canonical.Hash(source.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	mask := func(pixel v2input.Ref, selector string, protected bool) review.MaskRef {
		return review.MaskRef{Artifact: artifact(pixel), Canvas: source.Canvas, TransformChain: source.TransformChain,
			Selector: selector, Confidence: 1, Protected: protected}
	}
	targetRaw := marshal(mask(targetPixelRef, "glasses", false))
	protectedRaw := marshal(mask(protectedPixelRef, "protected", true))
	paramsRaw := marshal(map[string]any{"schema_version": 1, "base": base, "mode": "donor",
		"glasses_mask": ref(targetRaw), "protected": ref(protectedRaw), "model_lock_sha": strings.Repeat("a", 64)})
	candidate := RestorationReviewCandidate{Pixels: outputPixelRef, ChangedPixels: changedPixelRef}
	derived, err := RestorationResultArrayRefs(context.Background(), "portrait.glare", paramsRaw,
		sourceRaw, targetRaw, protectedRaw, candidate)
	if err != nil || derived.Source.Ref != sourcePixelRef || derived.Output.Ref != outputPixelRef ||
		derived.Target.Ref != targetPixelRef || derived.Protected.Ref != protectedPixelRef ||
		derived.Changed.Ref != changedPixelRef || derived.Generated != nil {
		t.Fatal("document-derived array inventory rejected", err)
	}
	stream := func(value v2input.Ref, raw []byte) RestorationPinnedArray {
		return RestorationPinnedArray{Ref: value, Data: masknpy.PinnedMask{Reader: bytes.NewReader(raw), Bytes: int64(len(raw)), SHA256: value.SHA256}}
	}
	streams := func() RestorationPixelStreams {
		return RestorationPixelStreams{
			Source: stream(sourcePixelRef, sourceBytes), Output: stream(outputPixelRef, outputBytes),
			Target: stream(targetPixelRef, targetBytes), Protected: stream(protectedPixelRef, protectedBytes),
			Changed: stream(changedPixelRef, changedBytes),
		}
	}
	count, err := VerifyRestorationResultPixels(context.Background(), "portrait.glare", paramsRaw,
		sourceRaw, targetRaw, protectedRaw, candidate, streams())
	if err != nil || count != 1 {
		t.Fatal("valid stream closure refused", count, err)
	}
	wrong := streams()
	wrong.Output.Ref.ArtifactID = uuid.New()
	if count, err = VerifyRestorationResultPixels(context.Background(), "portrait.glare", paramsRaw,
		sourceRaw, targetRaw, protectedRaw, candidate, wrong); err == nil || count != 0 {
		t.Fatal("substituted output reference accepted", count, err)
	}
	candidate.Result = ref([]byte("canonical-result"))
	scope := RestorationArrayScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(),
		Root: candidate.Result.ArtifactID, RootSHA: candidate.Result.SHA256}
	catalog := &restorationArrayCatalogFixture{objects: map[string]objectstore.Object{}, reads: map[string]int{}}
	reader := restorationArrayReaderFixture{}
	for _, item := range []struct {
		role string
		ref  v2input.Ref
		raw  []byte
	}{
		{"source_pixels", sourcePixelRef, sourceBytes}, {"output_pixels", outputPixelRef, outputBytes},
		{"target_pixels", targetPixelRef, targetBytes}, {"protected_pixels", protectedPixelRef, protectedBytes},
		{"changed_pixels", changedPixelRef, changedBytes},
	} {
		catalog.objects[item.role] = objectstore.Object{Key: item.role, VersionID: "v1",
			SHA256: item.ref.SHA256, ContentType: "application/x-npy", Bytes: int64(len(item.raw))}
		reader[item.role] = item.raw
	}
	if count, err = VerifyRestorationResultObjects(context.Background(), scope, catalog, reader,
		"portrait.glare", paramsRaw, sourceRaw, targetRaw, protectedRaw, candidate, streams()); err != nil || count != 1 {
		t.Fatal("versioned result refused", count, err)
	}
	catalog.reads = map[string]int{}
	catalog.change = true
	if count, err = VerifyRestorationResultObjects(context.Background(), scope, catalog, reader,
		"portrait.glare", paramsRaw, sourceRaw, targetRaw, protectedRaw, candidate, streams()); err == nil || count != 0 {
		t.Fatal("changed catalog object version accepted", count, err)
	}
}
