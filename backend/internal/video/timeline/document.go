package timeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
)

// Doc is a timeline document (DS-09) as a decoded JSON tree. A generic tree
// keeps command results byte-identical to the shared vectors (no zero-value
// fields appear), while Validate enforces the closed schema.
type Doc = map[string]any

// Parse decodes a document; trailing data is rejected.
func Parse(raw []byte) (Doc, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	doc, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("document must be an object")
	}
	return doc, nil
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func arr(v any) []any          { a, _ := v.([]any); return a }
func str(v any) string         { s, _ := v.(string); return s }

func isInt(v any) bool {
	f, ok := v.(float64)
	return ok && f == math.Trunc(f) && math.Abs(f) <= 9007199254740991
}
func toInt(v any) int64 { f, _ := v.(float64); return int64(f) }
func isNum(v any) bool  { f, ok := v.(float64); return ok && !math.IsNaN(f) && !math.IsInf(f, 0) }

func clone(d Doc) Doc {
	b, _ := json.Marshal(d)
	out, _ := Parse(b)
	return out
}

func fpsOf(d Doc) (int64, int64) {
	f := obj(obj(d["video"])["fps"])
	return toInt(f["num"]), toInt(f["den"])
}

// ClipDuration is FN-T-04.
func ClipDuration(c map[string]any) int64 {
	if str(obj(c["source"])["kind"]) == "photo" {
		return toInt(c["duration"])
	}
	n, den := int64(1), int64(1)
	if sp := obj(c["speed"]); sp != nil {
		n, den = toInt(sp["num"]), toInt(sp["den"])
	}
	return (toInt(c["src_out"]) - toInt(c["src_in"])) * den / n
}
