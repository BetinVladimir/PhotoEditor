package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestManualMaskOriginalParamsBinding(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var root rasterizedFaceMask
	if err := decodeExact(raw, &root); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: root.Image, Canvas: root.Target.Canvas, TransformChain: root.Target.TransformChain}
	vectors := manualVectors{Version: "manual_mask_v1", SourceSHA: source.Canvas.SourceSHA256, Canvas: source.Canvas, Shapes: []manualShape{}}
	params := manualRasterParams{Image: root.Image, Inline: &vectors, ObjectID: root.Target.ObjectID}
	body, _ := canonical.Marshal(params)
	sum := sha256.Sum256(body)
	ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	root.Vectors = ref
	rootBody, _ := canonical.Marshal(root)
	sum = sha256.Sum256(rootBody)
	rootRef := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	if external, err := VerifyManualMaskParams(body, ref, rootBody, rootRef, root.Target, source); err != nil || external != nil {
		t.Fatal(external, err)
	}
	params.ObjectID = nil
	body, _ = canonical.Marshal(params)
	sum = sha256.Sum256(body)
	ref.SHA256 = hex.EncodeToString(sum[:])
	root.Vectors = ref
	rootBody, _ = canonical.Marshal(root)
	sum = sha256.Sum256(rootBody)
	rootRef.SHA256 = hex.EncodeToString(sum[:])
	if _, err := VerifyManualMaskParams(body, ref, rootBody, rootRef, root.Target, source); err == nil {
		t.Fatal("mismatched object accepted")
	}
	radius, hardness := .1, 1.
	shape := manualShape{Kind: "stroke", Sequence: 0, Mode: "add", Points: []manualPoint{{X: .5, Y: .5}}, Radius: &radius, Hardness: &hardness}
	vectors.Shapes = []manualShape{shape}
	if !validManualVectors(vectors, source) {
		t.Fatal("valid stroke rejected")
	}
	vectors.Shapes = append(vectors.Shapes, shape)
	if validManualVectors(vectors, source) {
		t.Fatal("duplicate sequence accepted")
	}
	vectors.Shapes = vectors.Shapes[:1]
	vectors.Shapes[0].Points[0].X = math.Inf(1)
	if validManualVectors(vectors, source) {
		t.Fatal("nonfinite point accepted")
	}
}

func TestExternalManualVectorsOriginalBinding(t *testing.T) {
	rootBody, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var root rasterizedFaceMask
	if err := decodeExact(rootBody, &root); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: root.Image, Canvas: root.Target.Canvas, TransformChain: root.Target.TransformChain}
	vectors := manualVectors{Version: "manual_mask_v1", SourceSHA: source.Canvas.SourceSHA256, Canvas: source.Canvas, Shapes: []manualShape{}}
	body, _ := canonical.Marshal(vectors)
	sum := sha256.Sum256(body)
	ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	if err := VerifyManualVectorsDocument(body, ref, source); err != nil {
		t.Fatal(err)
	}
	if VerifyManualVectorsDocument(append(append([]byte{}, body...), ' '), ref, source) == nil {
		t.Fatal("changed original bytes accepted")
	}
	vectors.Canvas.Width++
	body, _ = canonical.Marshal(vectors)
	sum = sha256.Sum256(body)
	ref.SHA256 = hex.EncodeToString(sum[:])
	if VerifyManualVectorsDocument(body, ref, source) == nil {
		t.Fatal("foreign canvas accepted")
	}
	vectors.Canvas = source.Canvas
	vectors.Shapes = nil
	body, _ = canonical.Marshal(vectors)
	sum = sha256.Sum256(body)
	ref.SHA256 = hex.EncodeToString(sum[:])
	if VerifyManualVectorsDocument(body, ref, source) == nil {
		t.Fatal("null shape list accepted")
	}
	vectors.Shapes = []manualShape{}
	body, _ = canonical.Marshal(vectors)
	sum = sha256.Sum256(body)
	ref.SHA256 = hex.EncodeToString(sum[:])
	matrix := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	steps := []transformStep{{Kind: "matrix", From: source.Canvas, To: source.Canvas, Invertibility: "invertible", ValidRegion: rectangle{Width: source.Canvas.Width, Height: source.Canvas.Height}, Matrix: &matrix}}
	hash, err := canonical.Hash(struct {
		Steps []transformStep `json:"steps"`
	}{steps})
	if err != nil {
		t.Fatal(err)
	}
	source.TransformChain, _ = canonical.Marshal(transformChain{Steps: steps, Hash: hash})
	if err := validateTransformChain(source.TransformChain, source.Canvas); err != nil {
		t.Fatal("invalid test transform", err)
	}
	if VerifyManualVectorsDocument(body, ref, source) == nil {
		t.Fatal("transformed source accepted for source rasterization")
	}
}

func TestManualGeometryRequiresExplicitZeroFields(t *testing.T) {
	for _, raw := range []string{
		`{"shapes":[{"points":[{"x":0,"y":0}]}]}`,
		`{"shapes":[{"sequence":null,"points":[{"x":0,"y":0}]}]}`,
		`{"shapes":[{"sequence":0,"points":[{"y":0}]}]}`,
		`{"shapes":[{"sequence":0,"points":[{"x":null,"y":0}]}]}`,
	} {
		if manualGeometryFieldsPresent([]byte(raw)) {
			t.Fatal("missing/null geometry accepted", raw)
		}
	}
	if !manualGeometryFieldsPresent([]byte(`{"shapes":[{"sequence":0,"points":[{"x":0,"y":0}]}]}`)) {
		t.Fatal("explicit zero rejected")
	}
}

func TestExternalManualVectorsRejectMissingPointAndSequence(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var root rasterizedFaceMask
	if err := decodeExact(raw, &root); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: root.Image, Canvas: root.Target.Canvas, TransformChain: root.Target.TransformChain}
	for _, shape := range []map[string]any{
		{"kind": "stroke", "sequence": 0, "mode": "add", "points": []any{map[string]any{"x": 0, "y": 0}}, "radius": 0.1, "hardness": 1},
		{"kind": "stroke", "sequence": 0, "mode": "add", "points": []any{map[string]any{"y": 0}}, "radius": 0.1, "hardness": 1},
		{"kind": "stroke", "mode": "add", "points": []any{map[string]any{"x": 0, "y": 0}}, "radius": 0.1, "hardness": 1},
	} {
		body, err := canonical.Marshal(map[string]any{"version": "manual_mask_v1", "source_sha256": source.Canvas.SourceSHA256, "canvas": source.Canvas, "shapes": []any{shape}})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		err = VerifyManualVectorsDocument(body, ref, source)
		_, sequence := shape["sequence"]
		point := shape["points"].([]any)[0].(map[string]any)
		_, x := point["x"]
		if (err == nil) != (sequence && x) {
			t.Fatal("unexpected vector acceptance", shape, err)
		}
	}
}

func TestManualVectorsCanvasParserUsesStrictGeometry(t *testing.T) {
	rootBody, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var root rasterizedFaceMask
	if decodeExact(rootBody, &root) != nil {
		t.Fatal("fixture")
	}
	radius, hardness := .1, 1.
	vectors := manualVectors{Version: "manual_mask_v1", SourceSHA: root.Target.Canvas.SourceSHA256, Canvas: root.Target.Canvas, Shapes: []manualShape{{Kind: "stroke", Sequence: 0, Mode: "add", Points: []manualPoint{{X: 0, Y: 0}}, Radius: &radius, Hardness: &hardness}}}
	raw, err := canonical.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := ParseManualVectorsCanvas(raw)
	if err != nil || canvas != vectors.Canvas {
		t.Fatal(canvas, err)
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		t.Fatal("decode")
	}
	shape := body["shapes"].([]any)[0].(map[string]any)
	delete(shape, "sequence")
	raw, _ = canonical.Marshal(body)
	if _, err := ParseManualVectorsCanvas(raw); err == nil {
		t.Fatal("missing sequence accepted")
	}
}

func TestManualRasterDocumentParserChecksRootBytesAndDistinctOutputs(t *testing.T) {
	raw, err := os.ReadFile("testdata/face-manual-root-python.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	root, err := ParseManualRasterDocument(raw, ref)
	if err != nil {
		t.Fatal(err)
	}
	if !root.Protected.Protected || root.Target.Protected || root.Vectors.ID == uuid.Nil {
		t.Fatal("root metadata lost")
	}
	if _, err := ParseManualRasterDocument(append(append([]byte{}, raw...), ' '), ref); err == nil {
		t.Fatal("changed bytes accepted")
	}
	var doc rasterizedFaceMask
	if decodeExact(raw, &doc) != nil {
		t.Fatal("fixture")
	}
	doc.ProtectedPNG = doc.TargetPNG
	raw, _ = canonical.Marshal(doc)
	sum = sha256.Sum256(raw)
	ref.SHA256 = hex.EncodeToString(sum[:])
	if _, err := ParseManualRasterDocument(raw, ref); err == nil {
		t.Fatal("duplicate outputs accepted")
	}
}
