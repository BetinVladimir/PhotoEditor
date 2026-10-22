package v2render

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// compileAuthoredGeometryPreparationParams has no replacement Params/source
// argument. All direct roots come from the current prefix and protected native
// reader. The owning SQL Params publisher reconstructs this document again.
func compileAuthoredGeometryPreparationParams(prefix AuthoredGeometryPrefix, parallel v2input.Ref, native CurrentAuthoredGeometryNative) (CompiledAuthoredGeometryTaskParams, error) {
	fail := func() (CompiledAuthoredGeometryTaskParams, error) {
		return CompiledAuthoredGeometryTaskParams{}, v2input.ErrInvalid
	}
	s := prefix.Context
	if s.Instruction.Kind != "geometry" || s.Instruction.GeometryLayer == nil || len(prefix.Phases) != s.Instruction.Ordinal-1 || len(native.proof) == 0 {
		return fail()
	}
	var stack struct {
		Layers []json.RawMessage `json:"layers"`
	}
	if json.Unmarshal([]byte(s.LayerStackJCS), &stack) != nil {
		return fail()
	}
	var selected struct {
		ID        uuid.UUID   `json:"id"`
		Params    v2input.Ref `json:"params"`
		Operation string      `json:"operation"`
		Version   string      `json:"operation_version"`
		Enabled   bool        `json:"enabled"`
		Opacity   float64     `json:"opacity"`
	}
	found := false
	for _, raw := range stack.Layers {
		var candidate struct {
			ID uuid.UUID `json:"id"`
		}
		if json.Unmarshal(raw, &candidate) != nil {
			return fail()
		}
		if candidate.ID != *s.Instruction.GeometryLayer {
			continue
		}
		if found || json.Unmarshal(raw, &selected) != nil {
			return fail()
		}
		found = true
	}
	if !found || !selected.Enabled || selected.Opacity <= 0 || selected.Opacity > 1 || selected.Version != "1" || selected.Operation != native.witness.Task.Operation {
		return fail()
	}
	var proof struct {
		Native AuthoredGeometryPhaseWitness `json:"native"`
	}
	if json.Unmarshal(native.proof, &proof) != nil {
		return fail()
	}
	a, e := canonical.Marshal(proof.Native)
	if e != nil {
		return fail()
	}
	b, e := canonical.Marshal(native.witness)
	if e != nil || !bytes.Equal(a, b) {
		return fail()
	}
	// These exact documents were physically read and hashed by the native reader.
	for _, ref := range []v2input.Ref{native.witness.Root, native.witness.Image, native.witness.Report, native.witness.Task.Params} {
		if len(native.documents[ref]) == 0 {
			return fail()
		}
	}
	refs := make([]v2input.Ref, 0, len(prefix.Phases))
	for i, phase := range prefix.Phases {
		if phase.Ordinal != i+1 {
			return fail()
		}
		refs = append(refs, phase.Receipt)
	}
	return CompileAuthoredGeometryTaskParams(AuthoredGeometryTaskParams{SchemaVersion: 1, TenantID: s.Tenant, ParentRunID: s.Parent, WorkflowID: s.Workflow, InstructionID: s.Instruction.Identity, Ordinal: s.Instruction.Ordinal, ProgramSHA256: s.ProgramSHA, PredecessorSHA256: s.PredecessorSHA, Recipe: s.OriginalRecipe, BaseReceipt: prefix.BaseReceipt, Source: s.Source, ParallelSource: parallel, AuthoredParams: selected.Params, Native: native.witness, PhaseReceipts: refs})
}
