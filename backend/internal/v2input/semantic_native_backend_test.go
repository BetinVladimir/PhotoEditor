package v2input

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSemanticNativeQualifiedBackendProfiles(t *testing.T) {
	type compiler func(int64, int64, int64, SemanticQualifiedCosts, int64, int64, int64, int64) (json.RawMessage, error)
	type selected func(string, int64, int64, int64, SemanticQualifiedCosts, int64, int64, int64, int64) (json.RawMessage, error)
	cases := []struct {
		name     string
		old      compiler
		explicit selected
	}{
		{"person", CompileSemanticPersonProfile, CompileSemanticPersonProfileForBackend},
		{"detector", CompileSemanticDetectorProfile, CompileSemanticDetectorProfileForBackend},
		{"landmark", CompileSemanticLandmarkProfile, CompileSemanticLandmarkProfileForBackend},
		{"parse", CompileSemanticFaceParseProfile, CompileSemanticFaceParseProfileForBackend},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cost := SemanticQualifiedCosts{RAMBytes: 1 << 20, VRAMBytes: 11 << 30, ScratchBytes: 1 << 20}
			const limit int64 = 64 << 30
			legacy, e := c.old(32, 32, 1024, cost, limit, limit, limit, limit)
			if e != nil {
				t.Fatal(e)
			}
			cuda, e := c.explicit("cuda", 32, 32, 1024, cost, limit, limit, limit, limit)
			if e != nil || !bytes.Equal(cuda, legacy) {
				t.Fatal("default CUDA bytes changed", e)
			}
			raw, e := c.explicit("mps", 32, 32, 1024, cost, limit, limit, limit, limit)
			if e != nil {
				t.Fatal(e)
			}
			var v sessionDescribeExecutionProfile
			if json.Unmarshal(raw, &v) != nil || v.Backend != "mps" || v.ResourceEstimate.VRAMBytes != cost.VRAMBytes {
				t.Fatal("qualified device identity/cost lost")
			}
			if raw, e = c.explicit("mps", 32, 32, 1024, cost, limit, cost.VRAMBytes-1, limit, limit); e == nil || raw != nil {
				t.Fatal("insufficient measured device capacity admitted")
			}
			for _, bad := range []string{"", "cpu", "metal", "CUDA"} {
				if raw, e = c.explicit(bad, 32, 32, 1024, cost, limit, limit, limit, limit); e == nil || raw != nil {
					t.Fatal("unreviewed backend", bad)
				}
			}
		})
	}
}
