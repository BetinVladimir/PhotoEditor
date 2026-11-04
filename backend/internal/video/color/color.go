// Package color implements the colour maths of docs/video (FN-L-01…L-08): sRGB transfer, exposure, CDL, PCHIP curves,
// .cube parsing/writing and the folding of a clip's colour effects into one 3D LUT that ffmpeg applies with lut3d.
package color

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	LutMaxBytes = 20 << 20
	MinSize     = 17
	MaxSize     = 65
	FoldSize    = 65
	curveNodes  = 4096
)

func clamp01(x float64) float64 { return math.Min(1, math.Max(0, x)) }
func round6(x float64) float64  { return math.Round(x*1e6) / 1e6 }

// SRGBToLinear is FN-L-01.
func SRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// LinearToSRGB is FN-L-01 (the input is clamped to [0; 1] first).
func LinearToSRGB(c float64) float64 {
	c = clamp01(c)
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

type RGB [3]float64

// Exposure is FN-L-02.
func Exposure(c RGB, ev float64) RGB {
	var o RGB
	k := math.Pow(2, ev)
	for i := range c {
		o[i] = round6(LinearToSRGB(SRGBToLinear(c[i]) * k))
	}
	return o
}

// CDL is FN-L-03.
func CDL(c, slope, offset, power RGB, sat float64) RGB {
	var v RGB
	for i := range c {
		v[i] = math.Pow(math.Max(c[i]*slope[i]+offset[i], 0), power[i])
	}
	y := 0.2126*v[0] + 0.7152*v[1] + 0.0722*v[2]
	var o RGB
	for i := range v {
		o[i] = round6(clamp01(y + sat*(v[i]-y)))
	}
	return o
}

// Curve is a PCHIP curve sampled on 4096 nodes (FN-L-04).
type Curve struct {
	x0, x1 float64
	y      []float64
}

var ErrCurveRange = errors.New("CURVE_RANGE")

// NewCurve builds the curve; x must strictly increase.
func NewCurve(points [][2]float64) (*Curve, error) {
	n := len(points)
	if n < 2 {
		return nil, errors.New("CURVE_POINTS")
	}
	h, d := make([]float64, n-1), make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		h[i] = points[i+1][0] - points[i][0]
		if h[i] <= 0 {
			return nil, errors.New("CURVE_POINTS")
		}
		d[i] = (points[i+1][1] - points[i][1]) / h[i]
	}
	m := make([]float64, n)
	if n == 2 {
		m[0], m[1] = d[0], d[0]
	} else {
		for k := 1; k < n-1; k++ {
			if d[k-1]*d[k] <= 0 {
				continue
			}
			w1, w2 := 2*h[k]+h[k-1], h[k]+2*h[k-1]
			m[k] = (w1 + w2) / (w1/d[k-1] + w2/d[k])
		}
		m[0] = edge(h[0], h[1], d[0], d[1])
		m[n-1] = edge(h[n-2], h[n-3], d[n-2], d[n-3])
	}
	c := &Curve{x0: points[0][0], x1: points[n-1][0], y: make([]float64, curveNodes)}
	seg := 0
	for i := range c.y {
		x := c.x0 + (c.x1-c.x0)*float64(i)/float64(curveNodes-1)
		for seg < n-2 && x > points[seg+1][0] {
			seg++
		}
		t := (x - points[seg][0]) / h[seg]
		t2, t3 := t*t, t*t*t
		c.y[i] = (2*t3-3*t2+1)*points[seg][1] + (t3-2*t2+t)*h[seg]*m[seg] + (-2*t3+3*t2)*points[seg+1][1] + (t3-t2)*h[seg]*m[seg+1]
	}
	return c, nil
}

// edge is scipy's one-sided, shape-preserving end derivative.
func edge(h0, h1, d0, d1 float64) float64 {
	m := ((2*h0+h1)*d0 - h0*d1) / (h0 + h1)
	switch {
	case math.Signbit(m) != math.Signbit(d0):
		return 0
	case math.Signbit(d0) != math.Signbit(d1) && math.Abs(m) > 3*math.Abs(d0):
		return 3 * d0
	}
	return m
}

// Eval returns the curve at x; outside the point range the end values hold (the editor keeps curves inside [0; 1]).
func (c *Curve) Eval(x float64) float64 {
	if x <= c.x0 {
		return c.y[0]
	}
	if x >= c.x1 {
		return c.y[curveNodes-1]
	}
	p := (x - c.x0) / (c.x1 - c.x0) * float64(curveNodes-1)
	i := int(p)
	f := p - float64(i)
	return c.y[i]*(1-f) + c.y[i+1]*f
}

// Lut3D is a cube; the node (r, g, b) sits at index r + N*g + N*N*b.
type Lut3D struct {
	Size     int
	Min, Max RGB
	Data     []RGB
}

func Identity(size int) *Lut3D {
	l := &Lut3D{Size: size, Max: RGB{1, 1, 1}, Data: make([]RGB, size*size*size)}
	for b := 0; b < size; b++ {
		for g := 0; g < size; g++ {
			for r := 0; r < size; r++ {
				l.Data[r+size*g+size*size*b] = RGB{float64(r) / float64(size-1), float64(g) / float64(size-1), float64(b) / float64(size-1)}
			}
		}
	}
	return l
}

// Build is FN-L-05.
func Build(size int, f func(RGB) RGB) *Lut3D {
	l := Identity(size)
	for i, v := range l.Data {
		l.Data[i] = f(v)
	}
	return l
}

// ParseCube is FN-L-06.
func ParseCube(text string) (*Lut3D, error) {
	if len(text) > LutMaxBytes {
		return nil, errors.New("LUT_INVALID: too large")
	}
	l := &Lut3D{Max: RGB{1, 1, 1}}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 1024), 1024)
	var rows []RGB
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if len(sc.Text()) > 256 {
			return nil, errors.New("LUT_INVALID: line too long")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "TITLE":
			continue
		case "LUT_1D_SIZE", "LUT_1D_INPUT_RANGE":
			return nil, errors.New("LUT_INVALID: 1D")
		case "LUT_3D_SIZE":
			n, err := strconv.Atoi(strings.Join(f[1:], ""))
			if err != nil || n < MinSize || n > MaxSize {
				return nil, errors.New("LUT_INVALID: size")
			}
			l.Size = n
			continue
		case "DOMAIN_MIN", "DOMAIN_MAX":
			v, err := triple(f[1:])
			if err != nil {
				return nil, err
			}
			if f[0] == "DOMAIN_MIN" {
				l.Min = v
			} else {
				l.Max = v
			}
			continue
		}
		v, err := triple(f)
		if err != nil {
			return nil, err
		}
		rows = append(rows, v)
	}
	if err := sc.Err(); err != nil {
		return nil, errors.New("LUT_INVALID: " + err.Error())
	}
	if l.Size == 0 || len(rows) != l.Size*l.Size*l.Size {
		return nil, errors.New("LUT_INVALID: row count")
	}
	for _, r := range rows {
		for i := range r {
			if r[i] < l.Min[i] || r[i] > l.Max[i] {
				return nil, errors.New("LUT_INVALID: value outside domain")
			}
		}
	}
	l.Data = rows
	return l, nil
}

func triple(f []string) (RGB, error) {
	var v RGB
	if len(f) != 3 {
		return v, errors.New("LUT_INVALID: row")
	}
	for i := range v {
		x, err := strconv.ParseFloat(f[i], 64)
		if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
			return v, errors.New("LUT_INVALID: number")
		}
		v[i] = x
	}
	return v, nil
}

// WriteCube is FN-L-07.
func WriteCube(l *Lut3D) string {
	var b strings.Builder
	fmt.Fprintf(&b, "LUT_3D_SIZE %d\n", l.Size)
	for _, v := range l.Data {
		fmt.Fprintf(&b, "%.6f %.6f %.6f\n", v[0], v[1], v[2])
	}
	return b.String()
}

// Sample applies the cube to c by trilinear interpolation inside its domain.
func (l *Lut3D) Sample(c RGB) RGB {
	var p [3]float64
	var i0 [3]int
	for k := range c {
		span := l.Max[k] - l.Min[k]
		t := 0.0
		if span > 0 {
			t = (c[k] - l.Min[k]) / span
		}
		p[k] = clamp01(t) * float64(l.Size-1)
		i0[k] = int(math.Min(math.Floor(p[k]), float64(l.Size-2)))
		p[k] -= float64(i0[k])
	}
	n := l.Size
	at := func(r, g, b int) RGB { return l.Data[r+n*g+n*n*b] }
	var o RGB
	for dr := 0; dr < 2; dr++ {
		for dg := 0; dg < 2; dg++ {
			for db := 0; db < 2; db++ {
				w := lerpW(p[0], dr) * lerpW(p[1], dg) * lerpW(p[2], db)
				v := at(i0[0]+dr, i0[1]+dg, i0[2]+db)
				for k := range o {
					o[k] += w * v[k]
				}
			}
		}
	}
	return o
}

func lerpW(t float64, d int) float64 {
	if d == 1 {
		return t
	}
	return 1 - t
}

// Effect is one enabled colour effect of a clip or the master.
type Effect struct {
	Type   string
	Params map[string]any
	LUT    *Lut3D // parsed .cube for color.lut3d
}

var stageOrder = map[string]int{"color.basic": 0, "color.cdl": 1, "color.curves": 2, "color.lut3d": 3}

func num(m map[string]any, k string) float64 { f, _ := m[k].(float64); return f }

func vec3(v any) RGB {
	a, _ := v.([]any)
	var o RGB
	for i := 0; i < 3 && i < len(a); i++ {
		o[i], _ = a[i].(float64)
	}
	return o
}

// Fold is FN-L-08: one 65³ LUT for the effects, applied in the order basic → cdl → curves → lut3d (list order inside a type).
func Fold(effects []Effect, size int) (*Lut3D, error) {
	es := append([]Effect(nil), effects...)
	sort.SliceStable(es, func(i, j int) bool { return stageOrder[es[i].Type] < stageOrder[es[j].Type] })
	type stage func(RGB) RGB
	var stages []stage
	for _, e := range es {
		e := e
		switch e.Type {
		case "color.basic":
			stages = append(stages, func(c RGB) RGB { return Basic(c, e.Params) })
		case "color.cdl":
			slope, off, pow := vec3(e.Params["slope"]), vec3(e.Params["offset"]), vec3(e.Params["power"])
			sat := num(e.Params, "saturation")
			stages = append(stages, func(c RGB) RGB { return CDL(c, slope, off, pow, sat) })
		case "color.curves":
			var cs [4]*Curve // master, r, g, b
			for i, k := range []string{"master", "r", "g", "b"} {
				pts, ok := e.Params[k].([]any)
				if !ok {
					continue
				}
				ps := make([][2]float64, len(pts))
				for j, p := range pts {
					a, _ := p.([]any)
					if len(a) == 2 {
						ps[j][0], _ = a[0].(float64)
						ps[j][1], _ = a[1].(float64)
					}
				}
				c, err := NewCurve(ps)
				if err != nil {
					return nil, err
				}
				cs[i] = c
			}
			stages = append(stages, func(c RGB) RGB {
				for i := range c {
					if cs[0] != nil {
						c[i] = cs[0].Eval(c[i])
					}
					if cs[i+1] != nil {
						c[i] = cs[i+1].Eval(c[i])
					}
				}
				return c
			})
		case "color.lut3d":
			if e.LUT == nil {
				return nil, errors.New("LUT_MISSING")
			}
			k := num(e.Params, "intensity")
			stages = append(stages, func(c RGB) RGB {
				v := e.LUT.Sample(c)
				for i := range c {
					c[i] += k * (v[i] - c[i])
				}
				return c
			})
		default:
			return nil, fmt.Errorf("EFFECT_TYPE: %s", e.Type)
		}
	}
	return Build(size, func(c RGB) RGB {
		for _, s := range stages {
			c = s(c)
		}
		for i := range c {
			c[i] = clamp01(c[i])
		}
		return c
	}), nil
}

// Basic is color.basic. DEMO MVP: the spec fixes the parameter ranges and "operations in linear light in a fixed order"
// but gives no formulas, so these are this implementation's: exposure (FN-L-02), then white balance as linear gains,
// then tone and saturation on the display-referred value.
func Basic(c RGB, p map[string]any) RGB {
	if ev := num(p, "exposure_ev"); ev != 0 {
		c = Exposure(c, ev)
	}
	if t, tint := num(p, "temperature"), num(p, "tint"); t != 0 || tint != 0 {
		g := RGB{1 + 0.25*t/100, 1 - 0.15*tint/100, 1 - 0.25*t/100}
		for i := range c {
			c[i] = LinearToSRGB(SRGBToLinear(c[i]) * g[i])
		}
	}
	y := 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
	smooth := func(a, b, x float64) float64 { t := clamp01((x - a) / (b - a)); return t * t * (3 - 2*t) }
	if k := num(p, "contrast"); k != 0 {
		for i := range c {
			c[i] = 0.5 + (c[i]-0.5)*(1+k/100)
		}
	}
	// shadows/blacks lift the dark end, highlights/whites pull the bright end; the weights follow luma.
	d := 0.3*num(p, "shadows")/100*(1-smooth(0, 0.6, y)) + 0.15*num(p, "blacks")/100*(1-smooth(0, 0.25, y)) +
		0.3*num(p, "highlights")/100*smooth(0.4, 1, y) + 0.15*num(p, "whites")/100*smooth(0.75, 1, y)
	for i := range c {
		c[i] += d
	}
	y = 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
	if s := num(p, "saturation"); s != 0 {
		for i := range c {
			c[i] = y + (1+s/100)*(c[i]-y)
		}
	}
	if v := num(p, "vibrance"); v != 0 {
		mx, mn := math.Max(c[0], math.Max(c[1], c[2])), math.Min(c[0], math.Min(c[1], c[2]))
		low := 1.0
		if mx > 0 {
			low = 1 - clamp01((mx-mn)/mx)
		}
		for i := range c {
			c[i] = y + (1+v/100*low)*(c[i]-y)
		}
	}
	for i := range c {
		c[i] = clamp01(c[i])
	}
	return c
}
