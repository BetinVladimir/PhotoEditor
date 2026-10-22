// DTO fixtures only: no database, admission, qualification or work.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
)

func id(n byte) uuid.UUID            { var x uuid.UUID; x[15] = n; return x }
func ref(n byte) v2input.Ref         { return v2input.Ref{ArtifactID: id(n), SHA256: strings.Repeat("a", 64)} }
func ptr(x v2input.Ref) *v2input.Ref { return &x }
func main() {
	if len(os.Args) != 2 {
		panic("output directory required")
	}
	out := os.Args[1]
	put := func(n string, b []byte, e error) {
		if e != nil {
			panic(e)
		}
		if e = os.WriteFile(filepath.Join(out, n+".json"), b, 0600); e != nil {
			panic(e)
		}
	}
	src := v2input.SessionVersionBinding{AssetVersionID: id(5), Source: ref(13), CanvasHash: strings.Repeat("b", 64)}
	f, e := v2render.CompileAuthoredFinalParams(v2render.AuthoredFinalParams{SchemaVersion: 1, TenantID: id(1), ParentRunID: id(2), WorkflowID: id(3), InstructionID: id(4), ProgramSHA256: strings.Repeat("c", 64), Recipe: ref(10), BaseReceipt: ref(11), GeometryContinuation: ref(12), Source: src, RegisteredOriginal: ref(14), PhaseReceipts: []v2input.Ref{ref(15)}})
	put("final", f.JSON, e)
	s, e := v2render.CompileAuthoredSegmentParams(v2render.AuthoredSegmentParams{SchemaVersion: 1, TenantID: id(1), ParentRunID: id(2), WorkflowID: id(3), InstructionID: id(4), Ordinal: 1, ProgramSHA256: strings.Repeat("c", 64), PredecessorSHA256: strings.Repeat("d", 64), Recipe: ref(10), BaseReceipt: ref(11), Source: src, CurrentLayers: ptr(ref(15)), PhaseReceipts: []v2input.Ref{}})
	put("segment", s.JSON, e)
	s.Params.CurrentLayers = nil
	sn, e := v2render.CompileAuthoredSegmentParams(s.Params)
	put("segment_null_declaration", sn.JSON, e)
	ins, e := v2input.TaskID(id(3), "render.authored_geometry", 1)
	if e != nil {
		panic(e)
	}
	profile := `{"backend":"cpu"}`
	h := sha256.Sum256([]byte(profile))
	g, e := v2render.CompileAuthoredGeometryTaskParams(v2render.AuthoredGeometryTaskParams{SchemaVersion: 1, TenantID: id(1), ParentRunID: id(2), WorkflowID: id(3), InstructionID: ins, Ordinal: 1, ProgramSHA256: strings.Repeat("c", 64), PredecessorSHA256: strings.Repeat("d", 64), Recipe: ref(10), BaseReceipt: ref(11), Source: src, ParallelSource: ref(14), AuthoredParams: ref(15), PhaseReceipts: []v2input.Ref{}, Native: v2render.AuthoredGeometryPhaseWitness{Task: v2render.AuthoredPhaseTask{RunID: id(31), TaskID: id(32), ResultID: id(33), Operation: "optical.lens", Version: "1", Attempt: 1, Fence: 1, Params: ref(24), InputDigest: strings.Repeat("e", 64), ProfileJCS: profile, ProfileSHA: hex.EncodeToString(h[:]), Outputs: []v2input.Ref{ref(21), ref(22), ref(23)}}, Root: ref(21), Image: ref(22), Report: ref(23)}})
	put("geometry", g.JSON, e)
	g.Params.Native.Task.Operation = "image.upscale"
	g.Params.Native.Root = g.Params.Native.Image
	gu, e := v2render.CompileAuthoredGeometryTaskParams(g.Params)
	put("geometry_upscale", gu.JSON, e)
}
