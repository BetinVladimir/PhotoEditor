package timeline

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"photoeditor/backend/internal/video/testvectors"
)

type patch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}
type valCase struct {
	ID       string         `json:"id"`
	Patches  []patch        `json:"patches"`
	CtxPatch map[string]any `json:"ctx_patch"`
	Expect   []Issue        `json:"expect"`
}
type valVector struct {
	BaseDoc Doc            `json:"base_doc"`
	BaseCtx map[string]any `json:"base_ctx"`
	Cases   []valCase      `json:"cases"`
}

func deepCopy(t *testing.T, v any) any {
	t.Helper()
	b, _ := json.Marshal(v)
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func walk(t *testing.T, root any, parts []string) any {
	t.Helper()
	cur := root
	for _, p := range parts {
		if a, ok := cur.([]any); ok {
			i, _ := strconv.Atoi(p)
			cur = a[i]
		} else {
			cur = cur.(map[string]any)[p]
		}
	}
	return cur
}

func applyPatch(t *testing.T, doc Doc, p patch) {
	t.Helper()
	parts := strings.Split(strings.Trim(p.Path, "/"), "/")
	if p.Op == "append" {
		// slices are values: re-set the parent entry
		parent := walk(t, doc, parts[:len(parts)-1])
		last := parts[len(parts)-1]
		cur := walk(t, doc, parts).([]any)
		parent.(map[string]any)[last] = append(cur, p.Value)
		return
	}
	parent := walk(t, doc, parts[:len(parts)-1])
	last := parts[len(parts)-1]
	if a, ok := parent.([]any); ok {
		i, _ := strconv.Atoi(last)
		a[i] = p.Value
	} else {
		parent.(map[string]any)[last] = p.Value
	}
}

func toContext(m map[string]any) Context {
	var c Context
	ints := func(v any) map[string]int64 {
		out := map[string]int64{}
		for k, x := range v.(map[string]any) {
			out[k] = int64(x.(float64))
		}
		return out
	}
	strs := func(v any) map[string]string {
		out := map[string]string{}
		for k, x := range v.(map[string]any) {
			out[k] = x.(string)
		}
		return out
	}
	c.SourceDurations, c.SourceStates, c.SourceHashes = ints(m["sourceDurations"]), strs(m["sourceStates"]), strs(m["sourceHashes"])
	for _, x := range m["musicClipIds"].([]any) {
		c.MusicClipIDs = append(c.MusicClipIDs, x.(string))
	}
	c.ForRender, _ = m["forRender"].(bool)
	return c
}

func TestValidateVectors(t *testing.T) {
	var v valVector
	testvectors.Load(t, "validate_cases", &v)
	if len(v.Cases) != 21 {
		t.Fatalf("cases = %d", len(v.Cases))
	}
	for _, c := range v.Cases {
		doc := deepCopy(t, v.BaseDoc).(map[string]any)
		ctx := deepCopy(t, v.BaseCtx).(map[string]any)
		for _, p := range c.Patches {
			applyPatch(t, doc, p)
		}
		for k, x := range c.CtxPatch {
			ctx[k] = x
		}
		got := Validate(doc, toContext(ctx))
		if len(got) == 0 && len(c.Expect) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.Expect) {
			t.Errorf("%s: got %v want %v", c.ID, got, c.Expect)
		}
	}
}

func TestValidateCapsIssuesAndKeepsInput(t *testing.T) {
	var v valVector
	testvectors.Load(t, "validate_cases", &v)
	doc := deepCopy(t, v.BaseDoc).(map[string]any)
	clips := []any{}
	for i := 0; i < 150; i++ {
		c := deepCopy(t, walk(t, doc, []string{"tracks", "0", "clips", "0"})).(map[string]any)
		c["id"], c["at"] = "x"+strconv.Itoa(i), float64(0)
		clips = append(clips, c)
	}
	doc["tracks"].([]any)[0].(map[string]any)["clips"] = clips
	before, _ := json.Marshal(doc)
	if n := len(Validate(doc, toContext(v.BaseCtx))); n == 0 || n > 100 {
		t.Fatalf("issues = %d", n)
	}
	if after, _ := json.Marshal(doc); !bytes.Equal(before, after) {
		t.Fatal("document mutated")
	}
}

func TestValidateRangesFrom23(t *testing.T) {
	var v valVector
	testvectors.Load(t, "validate_cases", &v)
	doc := deepCopy(t, v.BaseDoc).(map[string]any)
	c := walk(t, doc, []string{"tracks", "0", "clips", "0"}).(map[string]any)
	c["transform"] = map[string]any{"scale": 9.0, "position": []any{0.0, 0.0}, "rotate": 0.0, "crop": []any{0.0, 0.0, 0.0, 0.0}}
	c["effects"] = []any{map[string]any{"id": "e1", "type": "color.cdl", "enabled": true, "params": map[string]any{
		"slope": []any{1.0, 1.0, 1.0}, "offset": []any{0.0, 0.0, 0.0}, "power": []any{1.0, 1.0, 1.0}, "saturation": 5.0}}}
	var ptrs []string
	for _, i := range Validate(doc, toContext(v.BaseCtx)) {
		ptrs = append(ptrs, i.Pointer)
	}
	want := []string{"/tracks/0/clips/0/transform/scale", "/tracks/0/clips/0/effects/0/params/saturation"}
	if !reflect.DeepEqual(ptrs, want) {
		t.Fatalf("pointers = %v", ptrs)
	}
}

type canonCase struct {
	Input     any    `json:"input"`
	Canonical string `json:"canonical"`
	SHA256    string `json:"sha256"`
}

func TestCanonicalVectors(t *testing.T) {
	var cs []canonCase
	testvectors.Load(t, "canonical", &cs)
	if len(cs) != 2 {
		t.Fatalf("cases = %d", len(cs))
	}
	for _, c := range cs {
		m, ok := c.Input.(map[string]any)
		if !ok {
			t.Fatal("input must be an object")
		}
		b, err := Canonicalize(m)
		if err != nil || string(b) != c.Canonical {
			t.Fatalf("canonical = %q, %v; want %q", b, err, c.Canonical)
		}
		if h, err := DocumentSHA256(m); err != nil || h != c.SHA256 {
			t.Fatalf("sha256 = %s, %v", h, err)
		}
	}
}

func TestParseRejectsTrailingAndNonObject(t *testing.T) {
	if _, err := Parse([]byte(`{} {}`)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := Parse([]byte(`[1]`)); err == nil {
		t.Fatal("array accepted")
	}
}
