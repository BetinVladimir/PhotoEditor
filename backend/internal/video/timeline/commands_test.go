package timeline

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"photoeditor/backend/internal/video/testvectors"
)

type cmdCase struct {
	ID     string         `json:"id"`
	Op     string         `json:"op"`
	Args   map[string]any `json:"args"`
	Expect struct {
		Error   string    `json:"error"`
		Doc     Doc       `json:"doc"`
		Applied []Applied `json:"applied"`
	} `json:"expect"`
}
type cmdVector struct {
	Base  Doc       `json:"base"`
	Cases []cmdCase `json:"cases"`
}

func i64(v any) int64 { f, _ := v.(float64); return int64(f) }
func s(v any) string  { x, _ := v.(string); return x }

func runCase(t *testing.T, c cmdCase, base Doc) (Doc, []Applied, error) {
	t.Helper()
	a := c.Args
	op := "apply_cuts"
	if f := strings.Fields(strings.ReplaceAll(c.Op, "(", " ")); len(f) > 0 {
		op = f[0]
	}
	switch op {
	case "split_clip":
		d, err := SplitClip(base, s(a["clip"]), i64(a["at"]), s(a["new_id"]))
		return d, nil, err
	case "trim_edge":
		d, err := TrimEdge(base, s(a["clip"]), s(a["edge"]), i64(a["pos"]), i64(a["source_duration"]))
		return d, nil, err
	case "move_clip":
		d, err := MoveClip(base, s(a["clip"]), i64(a["new_at"]), s(a["track"]))
		return d, nil, err
	case "delete_clip":
		d, err := DeleteClip(base, s(a["clip"]), a["ripple"] == true)
		return d, nil, err
	case "set_speed":
		d, err := SetSpeed(base, s(a["clip"]), i64(a["num"]), i64(a["den"]))
		return d, nil, err
	default: // apply_cuts, including the commands_extra vectors that carry no op
		var bs []Boundary
		for _, bv := range a["boundaries"].([]any) {
			b := bv.(map[string]any)
			bs = append(bs, Boundary{s(b["id"]), i64(b["start_ticks"]), i64(b["end_ticks"])})
		}
		var acc, ids []string
		for _, x := range a["accepted"].([]any) {
			acc = append(acc, x.(string))
		}
		for _, x := range a["new_ids"].([]any) {
			ids = append(ids, x.(string))
		}
		return ApplyCuts(base, s(a["media_id"]), bs, acc, s(a["mode"]), ids)
	}
}

func check(t *testing.T, c cmdCase, base Doc) Doc {
	t.Helper()
	before, _ := Canonicalize(base)
	got, applied, err := runCase(t, c, base)
	if c.Expect.Error != "" {
		if err == nil || err.Error() != c.Expect.Error {
			t.Fatalf("%s: err = %v, want %s", c.ID, err, c.Expect.Error)
		}
	} else {
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		g, _ := Canonicalize(got)
		w, _ := Canonicalize(c.Expect.Doc)
		if !bytes.Equal(g, w) {
			t.Fatalf("%s:\n got %s\nwant %s", c.ID, g, w)
		}
		if c.Expect.Applied != nil || strings.HasPrefix(c.Op, "apply_cuts") {
			if len(applied) != len(c.Expect.Applied) {
				t.Fatalf("%s: applied = %v, want %v", c.ID, applied, c.Expect.Applied)
			}
			for i := range applied {
				if applied[i] != c.Expect.Applied[i] {
					t.Fatalf("%s: applied[%d] = %v, want %v", c.ID, i, applied[i], c.Expect.Applied[i])
				}
			}
		}
	}
	if after, _ := Canonicalize(base); !bytes.Equal(before, after) {
		t.Fatalf("%s: input mutated", c.ID)
	}
	return got
}

func TestCommandVectors(t *testing.T) {
	var v cmdVector
	testvectors.Load(t, "commands", &v)
	if len(v.Cases) != 28 {
		t.Fatalf("cases = %d", len(v.Cases))
	}
	results := map[string]Doc{}
	for _, c := range v.Cases {
		base := v.Base
		switch c.ID {
		case "SP-06":
			d, err := SetSpeed(v.Base, "c2", 2, 1)
			if err != nil {
				t.Fatal(err)
			}
			base = d
		case "AC-03":
			base = results["AC-01"]
		}
		results[c.ID] = check(t, c, base)
	}
}

func TestCommandExtraVectors(t *testing.T) {
	var v cmdVector
	testvectors.Load(t, "commands_extra", &v)
	if len(v.Cases) != 4 {
		t.Fatalf("cases = %d", len(v.Cases))
	}
	results := map[string]Doc{}
	for _, c := range v.Cases {
		base := v.Base
		if c.ID == "TC-T-AC-M2" {
			base = results["TC-T-AC-M1"]
		}
		results[c.ID] = check(t, c, base)
	}
}

func TestApplyCutsIDsExhaustedIsTypedError(t *testing.T) {
	var v cmdVector
	testvectors.Load(t, "commands_extra", &v)
	c := v.Cases[3]
	if _, _, err := runCase(t, c, v.Base); !errors.Is(err, ErrIDsExhausted) {
		t.Fatalf("err = %v", err)
	}
}
