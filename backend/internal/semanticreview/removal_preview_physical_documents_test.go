package semanticreview

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"hash/crc32"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Real document hashes and float32 NPY bytes; no SQL authority or remote store.
// SQL authority is covered separately in integration scenarios.
func buildRemovalPhysicalDocuments(t *testing.T, mutations ...string) removalPhysicalDocuments {
	mode := ""
	if len(mutations) > 0 {
		mode = mutations[0]
	}
	t.Helper()
	bodies := map[string][]byte{}
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	source := fixtures[0]
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		r := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		bodies[r.ArtifactID.String()] = append([]byte(nil), body...)
		return r
	}
	params := RemovalPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "remove_object", ModelID: "big_lama", ContextPixels: 128, FeatherPixels: 8}
	params.Source.Source = ref(source)
	params.Source.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	width, height := int(image.Canvas.Width), int(image.Canvas.Height)
	header := fmt.Sprintf("{'descr': '<f4', 'fortran_order': False, 'shape': (%d, %d, 3), }", height, width)
	padding := (16 - (10+len(header)+1)%16) % 16
	header += string(bytes.Repeat([]byte{' '}, padding)) + "\n"
	pixels := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(pixels[8:10], uint16(len(header)))
	pixels = append(pixels, []byte(header)...)
	pixels = append(pixels, make([]byte, width*height*3*4)...)
	pixelRef := ref(pixels)
	if err := masknpy.VerifyNonnegativeFloat32RGBNPY(context.Background(), bytes.NewReader(pixels), int64(len(pixels)), pixelRef.SHA256, int64(width), int64(height)); err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(source, &output); err != nil {
		t.Fatal(err)
	}
	output["pixels"] = pixelRef
	imageRaw, err := canonical.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	evidence := RemovalPreviewEvidence{SchemaVersion: 1, Params: params, WorkingImage: ref(imageRaw), Pixels: pixelRef, Algorithm: "big_lama_work_srgb_delta_v1", ProtectedPixelsUnchanged: true, OutsideMaskUnchanged: true, ChangedPixels: 0, GlobalPass: true, ROI: []int64{0, 0, int64(width), int64(height)}}
	evidenceRaw, err := canonical.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	evidenceRef := ref(evidenceRaw)
	decoded, err := DecodeRemovalPreviewEvidence(evidenceRef, evidenceRaw, params)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := VerifyRemovalPreviewImageBinding(decoded, source, imageRaw)
	if err != nil || metadata.Width != int64(width) || metadata.Height != int64(height) {
		t.Fatal(metadata, err)
	}
	icc, err := os.ReadFile("../../../resources/icc/srgb_v1.icc")
	if err != nil {
		t.Fatal(err)
	}
	iccSum := sha256.Sum256(icc)
	srgbSHA := hex.EncodeToString(iccSum[:])
	displayRef := func(role string) v2input.Ref {
		displayWidth := width
		if mode == "dimensions_"+role {
			displayWidth++
		}
		im := stdimage.NewNRGBA(stdimage.Rect(0, 0, displayWidth, height))
		c := color.NRGBA{120, 120, 120, 255}
		if role == "affected" {
			c = color.NRGBA{255, 80, 40, 128}
		}
		if role == "protected" {
			c = color.NRGBA{0, 220, 255, 128}
		}
		if mode == "color_"+role {
			c.R ^= 1
		}
		for y := 0; y < height; y++ {
			for x := 0; x < displayWidth; x++ {
				im.SetNRGBA(x, y, c)
			}
		}
		var out bytes.Buffer
		if err := png.Encode(&out, im); err != nil {
			t.Fatal(err)
		}
		raw := out.Bytes()
		if role == "before" || role == "after" {
			var compressed bytes.Buffer
			zw := zlib.NewWriter(&compressed)
			profileBytes := append([]byte(nil), icc...)
			if mode == "icc_"+role {
				profileBytes[len(profileBytes)-1] ^= 1
			}
			if _, err := zw.Write(profileBytes); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			payload := append([]byte{'I', 'C', 'C', 0, 0}, compressed.Bytes()...)
			chunk := make([]byte, 12+len(payload))
			binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
			copy(chunk[4:8], "iCCP")
			copy(chunk[8:], payload)
			binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
			raw = append(append(append([]byte(nil), raw[:33]...), chunk...), raw[33:]...)
		}
		return ref(raw)
	}
	presentation := RemovalPreviewPresentation{SchemaVersion: 1, Params: params, NumericalEvidence: evidenceRef, WorkingImage: evidence.WorkingImage, Pixels: pixelRef, Before: displayRef("before"), After: displayRef("after"), Affected: displayRef("affected"), Protected: displayRef("protected"), Width: width, Height: height, ICCLockSHA: params.Plan.SHA256, SRGBSHA: srgbSHA, Resize: "area_linear_premult_v1", Overlay: "max_coverage_ceil_alpha_v1"}
	presentationRaw, err := canonical.Marshal(presentation)
	if err != nil {
		t.Fatal(err)
	}
	verified := VerifiedRemovalPreviewEvidence{Evidence: decoded, Source: PlanSource{Ref: evidenceRef}}
	presentationRef := ref(presentationRaw)
	if _, err := DecodeRemovalPreviewPresentation(presentationRef, presentationRaw, verified, params.Plan.SHA256, srgbSHA); err != nil {
		t.Fatal(err)
	}
	return removalPhysicalDocuments{params: params, source: source, presentation: presentation, presentationRef: presentationRef, bodies: bodies, srgbSHA: srgbSHA}
}

type removalPhysicalDocuments struct {
	params          RemovalPreviewParams
	source          []byte
	presentation    RemovalPreviewPresentation
	presentationRef v2input.Ref
	bodies          map[string][]byte
	srgbSHA         string
}

func TestRemovalPhysicalDocumentChain(t *testing.T) {
	buildRemovalPhysicalDocuments(t)
}
