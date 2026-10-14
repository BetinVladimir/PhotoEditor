package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func downloadDigest(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func downloadExportFixture() DownloadExportSpec {
	quality := 95
	manifest := review.ArtifactRef{ID: uuid.New(), SHA256: downloadDigest("manifest")}
	pixels := review.ArtifactRef{ID: uuid.New(), SHA256: downloadDigest("pixels")}
	source := downloadDigest("source")
	return DownloadExportSpec{RunID: uuid.New(), TargetICCSHA256: downloadDigest("icc"),
		Preset: DownloadPreset{Format: "jpeg", ICCID: "srgb_v1", Quality: &quality, Subsampling: "444",
			Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip"},
		Items: []DownloadExportItem{{Ordinal: 0, Manifest: manifest, Image: review.WorkingImage{
			Pixels: pixels, Canvas: review.Canvas{Width: 400, Height: 200, Orientation: 1, SourceSHA256: source, SpaceID: "oriented:" + source},
			Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", ICCSHA256: downloadDigest("work"), Encoding: "gamma24"},
			DType: "float32",
		}}},
	}
}

func TestCompileDownloadExportsDeterministicAndExactInputs(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	spec := downloadExportFixture()
	first, err := CompileDownloadExports(spec, registry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileDownloadExports(spec, registry)
	if err != nil || len(first) != 1 || first[0].ID != second[0].ID || !bytes.Equal(first[0].Params, second[0].Params) {
		t.Fatal("download export compiler is not deterministic", first, second, err)
	}
	if len(first[0].Inputs) != 2 || first[0].Inputs[0] != spec.Items[0].Manifest || first[0].Inputs[1] != spec.Items[0].Image.Pixels {
		t.Fatal("nested image refs not bound exactly", first[0].Inputs)
	}
}

func TestCompileDownloadExportsRejectsUnsafePresetAndBrokenOrder(t *testing.T) {
	registry, _ := LoadRegistry()
	for _, mutate := range []func(*DownloadExportSpec){
		func(s *DownloadExportSpec) { s.Preset.MetadataPolicy = "safe_camera" },
		func(s *DownloadExportSpec) { s.Preset.Subsampling = "420" },
		func(s *DownloadExportSpec) { s.Items[0].Ordinal = 1 },
		func(s *DownloadExportSpec) { s.Items[0].Image.DType = "uint16" },
		func(s *DownloadExportSpec) { s.Items[0].Image.Pixels = s.Items[0].Manifest },
	} {
		spec := downloadExportFixture()
		mutate(&spec)
		if _, err := CompileDownloadExports(spec, registry); err == nil {
			t.Fatal("invalid download export accepted", spec)
		}
	}
}

func TestCompileDownloadRejectsOversizedTargetBeforePlanRegistration(t *testing.T) {
 registry,err:=LoadRegistry()
 if err!=nil { t.Fatal(err) }
 for _, mode:=range []string{"upscale","source_overflow","source_limit","valid_no_upscale"} {
  t.Run(mode,func(t *testing.T){
   spec:=downloadExportFixture()
   edge:=int64(100000)
   spec.Preset.Size=&ExportSize{LongEdge:&edge,Fit:"contain",AllowUpscale:mode!="valid_no_upscale"}
   if mode=="source_overflow" { spec.Items[0].Image.Canvas.Width=1<<62;spec.Items[0].Image.Canvas.Height=4 }
   if mode=="source_limit" { spec.Items[0].Image.Canvas.Width=20001;spec.Items[0].Image.Canvas.Height=10000 }
   nodes,err:=CompileDownloadExports(spec,registry)
   if mode=="valid_no_upscale" {
    if err!=nil || len(nodes)!=1 { t.Fatalf("bounded original refused: %v",err) }
   } else if err==nil || len(nodes)!=0 { t.Fatalf("unsafe target produced nodes: %v",err) }
  })
 }
}
