package interactive

import (
	"encoding/json"
	"testing"
)

func TestMaskModeUsesElementCounts(t *testing.T) {
	empty := json.RawMessage(`[]`)
	one := json.RawMessage(`[{"kind":"stroke","sequence":0,"mode":"add","points":[{"x":0,"y":1}],"radius":0.1,"hardness":1}]`)
	prompt := json.RawMessage(`[{"x":0,"y":1,"label":"positive"}]`)
	for _, req := range []MaskCreate{{Points: prompt, Strokes: empty, Polygons: empty}, {Strokes: one, Points: empty}, {Polygons: one, Strokes: empty, Points: empty}} {
		shapes, points, manual, err := parseMaskInputs(req)
		if err != nil || manual != (len(shapes) > 0) || (len(points) > 0) == manual {
			t.Fatal("valid exclusive input rejected", err)
		}
	}
	for _, req := range []MaskCreate{{}, {Points: empty, Strokes: empty}, {Points: prompt, Strokes: one}, {Points: prompt, Polygons: json.RawMessage(`null`)}, {Strokes: one, Points: json.RawMessage(`{}`)}} {
		shapes, points, _, err := parseMaskInputs(req)
		if err == nil || shapes != nil || points != nil {
			t.Fatal("ambiguous or malformed input accepted")
		}
	}
}

func TestMaskPromptRejectsInvalidCoordinatesAndLabels(t *testing.T) {
	for _, raw := range []string{`[{"x":-0.1,"y":0.5,"label":"positive"}]`, `[{"x":0.5,"y":2,"label":"negative"}]`, `[{"x":null,"y":0.5,"label":"positive"}]`, `[{"x":0.5,"y":0.5,"label":"other"}]`, `[{"x":0.5,"y":0.5,"label":"positive","extra":true}]`} {
		if _, _, _, err := parseMaskInputs(MaskCreate{Points: json.RawMessage(raw)}); err == nil {
			t.Fatal("invalid prompt point accepted", raw)
		}
	}
}

func TestPromptOptionsAndPositivePoint(t *testing.T) {
	positive := json.RawMessage(`[{"x":0,"y":1,"label":"positive"}]`)
	for _, req := range []MaskCreate{
		{Points: json.RawMessage(`[{"x":0,"y":1,"label":"negative"}]`)},
		{Points: positive, Selector: "bad selector"},
		{Points: positive, Box: json.RawMessage(`{"x0":0,"y0":0,"x1":0,"y1":1}`)},
		{Points: positive, Box: json.RawMessage(`{"x0":0,"y0":0,"x1":2,"y1":1}`)},
		{Points: positive, Box: json.RawMessage(`[]`)},
	} {
		if _, _, _, err := parseMaskInputs(req); err == nil {
			t.Fatal("invalid prompt accepted")
		}
	}
	for _, box := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`{"x0":0,"y0":0,"x1":1,"y1":1}`)} {
		if _, _, _, err := parseMaskInputs(MaskCreate{Points: positive, Selector: "face:1", Box: box}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManualShapeGeometry(t *testing.T) {
	valid := `{"kind":"stroke","sequence":0,"mode":"add","points":[{"x":0,"y":1}],"radius":0.1,"hardness":1}`
	polygon := `{"kind":"polygon","sequence":1,"mode":"protect","points":[{"x":0,"y":0},{"x":1,"y":0},{"x":1,"y":1}]}`
	if _, _, _, err := parseMaskInputs(MaskCreate{Strokes: json.RawMessage("[" + valid + "]"), Polygons: json.RawMessage("[" + polygon + "]")}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`[{}]`, "[" + valid + "," + valid + "]",
		`[{"kind":"stroke","sequence":0.5,"mode":"add","points":[{"x":0,"y":1}],"radius":0.1,"hardness":1}]`,
		`[{"kind":"stroke","sequence":0,"mode":"add","points":[{"y":1}],"radius":0.1,"hardness":1}]`,
		`[{"kind":"stroke","sequence":0,"mode":"add","points":[{"x":0,"y":1}],"radius":0,"hardness":1}]`,
		`[{"kind":"polygon","sequence":0,"mode":"add","points":[{"x":0,"y":1}]}]`,
	} {
		if _, _, _, err := parseMaskInputs(MaskCreate{Strokes: json.RawMessage(raw)}); err == nil {
			t.Fatal("invalid geometry accepted", raw)
		}
	}
}
