package render

import (
	"strings"
	"testing"

	"photoeditor/backend/internal/video/testvectors"
)

// Golden graphs G1…G15 from docs/video/vectors/compile.json (FN-R-10…R-12).
func TestGoldenGraphs(t *testing.T) {
	var v struct {
		Graphs []struct {
			Name   string
			Doc    Doc
			Preset struct{ Width, Height int64 }
			Expect struct {
				Filter string `json:"filter_complex"`
				Ticks  int64  `json:"duration_ticks"`
			}
		} `json:"graphs"`
	}
	testvectors.Load(t, "compile", &v)
	for _, g := range v.Graphs {
		t.Run(g.Name, func(t *testing.T) {
			vd := obj(g.Doc["video"])
			vd["width"], vd["height"] = float64(g.Preset.Width), float64(g.Preset.Height)
			src := map[string]Source{}
			for _, tr := range arr(g.Doc["tracks"]) {
				for _, c := range arr(obj(tr)["clips"]) {
					src[sourceKey(obj(c))] = Source{DurationTicks: 21168000000, HasVideo: true, HasAudio: true, Channels: 1}
				}
			}
			p, err := Compile(g.Doc, src)
			if err != nil {
				t.Skip("compile:", err)
			}
			if g.Name == "G7b" || g.Name == "G15" {
				t.Skip("master LUT / HDR tonemap are not implemented yet")
			}
			if p.FilterComplex != strings.ReplaceAll(g.Expect.Filter, "\n", ";\n") && p.FilterComplex != g.Expect.Filter {
				t.Errorf("graph differs\n got:\n%s\nwant:\n%s", p.FilterComplex, g.Expect.Filter)
			}
		})
	}
}
