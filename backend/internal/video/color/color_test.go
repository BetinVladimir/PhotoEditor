package color

import (
	"math"
	"strings"
	"testing"

	"photoeditor/backend/internal/video/testvectors"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestVectors(t *testing.T) {
	var v struct {
		S2L []struct{ In, Out float64 } `json:"srgb_to_linear"`
		Exp []struct {
			RGB RGB
			EV  float64
			Out RGB
		} `json:"exposure"`
		CDL []struct {
			RGB, Slope, Offset, Power RGB
			Sat                       float64
			Out                       RGB
		} `json:"cdl"`
		Pchip struct {
			Points [][2]float64
			Eval   []struct{ X, Y float64 }
		} `json:"pchip"`
		Lut struct {
			Size    int
			Samples []struct {
				R, G, B, Index int
				Value          RGB
			}
		} `json:"lut"`
	}
	testvectors.Load(t, "color", &v)
	for _, c := range v.S2L {
		if !near(SRGBToLinear(c.In), c.Out, 1e-6) {
			t.Errorf("srgb_to_linear(%v)", c.In)
		}
	}
	for _, c := range v.Exp {
		got := Exposure(c.RGB, c.EV)
		for i := range got {
			if !near(got[i], c.Out[i], 1e-6) {
				t.Errorf("exposure %v ev=%v: %v want %v", c.RGB, c.EV, got, c.Out)
			}
		}
	}
	for _, c := range v.CDL {
		got := CDL(c.RGB, c.Slope, c.Offset, c.Power, c.Sat)
		for i := range got {
			if !near(got[i], c.Out[i], 1e-6) {
				t.Errorf("cdl %+v: %v", c, got)
			}
		}
	}
	cv, err := NewCurve(v.Pchip.Points)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range v.Pchip.Eval {
		if !near(cv.Eval(e.X), e.Y, 2e-4) { // 4096 linear nodes between exact PCHIP values
			t.Errorf("pchip(%v)=%v want %v", e.X, cv.Eval(e.X), e.Y)
		}
	}
	l := Build(v.Lut.Size, func(c RGB) RGB { return CDL(c, RGB{.5, .5, .5}, RGB{}, RGB{1, 1, 1}, 1) })
	for _, s := range v.Lut.Samples {
		got := l.Data[s.R+v.Lut.Size*s.G+v.Lut.Size*v.Lut.Size*s.B]
		if s.Index != s.R+v.Lut.Size*s.G+v.Lut.Size*v.Lut.Size*s.B {
			t.Errorf("index formula")
		}
		for i := range got {
			if !near(got[i], s.Value[i], 1e-6) {
				t.Errorf("lut node %+v: %v", s, got)
			}
		}
	}
}

func TestCubeRoundTripAndLimits(t *testing.T) {
	id := Identity(17)
	back, err := ParseCube(WriteCube(id))
	if err != nil || back.Size != 17 {
		t.Fatalf("%v", err)
	}
	for i, v := range id.Data {
		for k := range v {
			if !near(v[k], back.Data[i][k], 1e-6) {
				t.Fatalf("node %d", i)
			}
		}
	}
	for name, text := range map[string]string{
		"1d":     "LUT_1D_SIZE 17\n",
		"size":   "LUT_3D_SIZE 3\n",
		"rows":   "LUT_3D_SIZE 17\n0 0 0\n",
		"domain": "LUT_3D_SIZE 17\n" + strings.Repeat("2 0 0\n", 17*17*17),
	} {
		if _, err := ParseCube(text); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestFoldOrderAndIdentity(t *testing.T) {
	idl, err := Fold(nil, 17)
	if err != nil || idl.Data[5][0] != Identity(17).Data[5][0] {
		t.Fatal("empty fold must be the identity")
	}
	// CDL slope 0.5 followed by a half-intensity inversion LUT, listed in the "wrong" order: stage order wins.
	inv := Build(17, func(c RGB) RGB { return RGB{1 - c[0], 1 - c[1], 1 - c[2]} })
	l, err := Fold([]Effect{
		{Type: "color.lut3d", Params: map[string]any{"intensity": 1.0}, LUT: inv},
		{Type: "color.cdl", Params: map[string]any{"slope": []any{0.5, 0.5, 0.5}, "offset": []any{0.0, 0.0, 0.0}, "power": []any{1.0, 1.0, 1.0}, "saturation": 1.0}},
	}, 17)
	if err != nil {
		t.Fatal(err)
	}
	last := l.Data[len(l.Data)-1] // white -> cdl 0.5 -> inverted 0.5
	if !near(last[0], 0.5, 1e-3) || !near(l.Data[0][0], 1, 1e-3) {
		t.Errorf("folded nodes %v %v", last, l.Data[0])
	}
}

func TestBasicMonotoneAndBounded(t *testing.T) {
	p := map[string]any{"exposure_ev": 0.5, "contrast": 20.0, "saturation": 10.0, "temperature": -8.0}
	prev := -1.0
	for i := 0; i <= 20; i++ {
		x := float64(i) / 20
		o := Basic(RGB{x, x, x}, p)
		for _, c := range o {
			if c < 0 || c > 1 {
				t.Fatalf("out of range %v", o)
			}
		}
		if y := (o[0] + o[1] + o[2]) / 3; y < prev-1e-9 {
			t.Fatalf("not monotone at %v", x)
		} else {
			prev = y
		}
	}
	if o := Basic(RGB{.3, .4, .5}, map[string]any{}); o != (RGB{.3, .4, .5}) {
		t.Errorf("no params must be neutral: %v", o)
	}
}
