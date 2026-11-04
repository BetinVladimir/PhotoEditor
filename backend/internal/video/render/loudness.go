package render

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Measured is the loudnorm pass-1 report.
type Measured struct {
	InputI, InputTP, InputLRA, InputThresh, TargetOffset float64
}

// Silent reports a measurement of (near) digital silence, where loudnorm has
// nothing to normalise.
func (m Measured) Silent() bool { return math.IsInf(m.InputI, 0) || m.InputI < -70 }

func loudnorm(t, tp float64) string {
	return fmt.Sprintf("loudnorm=I=%s:TP=%s:LRA=11", fmtNum(t), fmtNum(tp))
}

// LoudnessPass1 builds the measuring command of section 7: audio graph only,
// output discarded, JSON report on stderr.
func LoudnessPass1(p Plan, paths map[string]string, t, tp float64) ([]string, error) {
	a := []string{"-hide_banner", "-nostdin", "-y"}
	for _, in := range p.Inputs {
		path, ok := paths[in.Source]
		if !ok {
			return nil, Error{Code: "COMPILE_SOURCE_MISSING", Clip: in.ClipID}
		}
		a = append(a, in.Options...)
		a = append(a, "-i", path)
	}
	g := p.AudioGraph + ";\n[abus]" + loudnorm(t, tp) + ":print_format=json[aout]"
	return append(a, "-filter_complex", g, "-map", "[aout]", "-f", "null", "-"), nil
}

// ParseMeasured reads the last JSON object of loudnorm's stderr.
func ParseMeasured(stderr []byte) (Measured, error) {
	end := -1
	for i := len(stderr) - 1; i >= 0; i-- {
		if stderr[i] == '}' {
			end = i
			break
		}
	}
	start := -1
	for i := end; i >= 0; i-- {
		if stderr[i] == '{' {
			start = i
			break
		}
	}
	if start < 0 || end < start {
		return Measured{}, fmt.Errorf("loudnorm report not found")
	}
	var raw map[string]string
	if err := json.Unmarshal(stderr[start:end+1], &raw); err != nil {
		return Measured{}, err
	}
	f := func(k string) (float64, error) { return strconv.ParseFloat(raw[k], 64) }
	var m Measured
	var err error
	for _, x := range []struct {
		k string
		d *float64
	}{{"input_i", &m.InputI}, {"input_tp", &m.InputTP}, {"input_lra", &m.InputLRA}, {"input_thresh", &m.InputThresh}, {"target_offset", &m.TargetOffset}} {
		if *x.d, err = f(x.k); err != nil {
			return Measured{}, fmt.Errorf("loudnorm %s: %w", x.k, err)
		}
	}
	return m, nil
}

// WithLoudness returns the plan whose audio bus is normalised in pass 2.
func WithLoudness(p Plan, t, tp float64, m Measured) Plan {
	n := fmt.Sprintf("%s:measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true:print_format=summary",
		loudnorm(t, tp), fmtNum(m.InputI), fmtNum(m.InputTP), fmtNum(m.InputLRA), fmtNum(m.InputThresh), fmtNum(m.TargetOffset))
	lim := fmtNumD(math.Pow(10, tp/20), 6)
	p.FilterComplex += fmt.Sprintf(";\n[abus]%s,alimiter=limit=%s:level=disabled[aout]", n, lim)
	p.Maps = []string{p.Maps[0], "[aout]"}
	return p
}
