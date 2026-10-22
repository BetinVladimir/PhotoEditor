package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type AuthoredGeometryOccurrence struct {
	Instruction      uuid.UUID                     `json:"instruction_id"`
	Phase            v2input.Ref                   `json:"phase"`
	CanonicalReceipt v2input.Ref                   `json:"canonical_receipt"`
	InputSource      v2input.SessionVersionBinding `json:"input_source"`
}
type AuthoredCompoundContinuation struct {
	SchemaVersion  int                           `json:"schema_version"`
	Tenant         uuid.UUID                     `json:"tenant_id"`
	Parent         uuid.UUID                     `json:"parent_run_id"`
	Recipe         v2input.Ref                   `json:"recipe"`
	OriginalSource v2input.SessionVersionBinding `json:"original_source"`
	ProgramSHA     string                        `json:"program_sha256"`
	BaseReceipt    v2input.Ref                   `json:"base_receipt"`
	Stages         []AuthoredGeometryOccurrence  `json:"stages"`
	FinalImage     v2input.Ref                   `json:"final_image"`
}

// CompileAuthoredFinalContinuation assembles content from actual physically
// read canonical occurrences. It is not publication/admission authority. The
// durable writer must independently repeat the SQL transcript before and after
// immutable IO; the native adopter must replay each operation-specific compound.
func CompileAuthoredFinalContinuation(documents AuthoredFinalDocuments) ([]byte, string, v2input.Ref, error) {
	fail := func() ([]byte, string, v2input.Ref, error) { return nil, "", v2input.Ref{}, v2input.ErrInvalid }
	inventory := documents.Inventory
	source := inventory.Context
	if source.Tenant == uuid.Nil || source.Parent == uuid.Nil || source.Instruction.Kind != "finalize" || len(inventory.Phases) != source.Instruction.Ordinal-1 {
		return fail()
	}
	read := func(ref v2input.Ref, expected string) ([]byte, bool) {
		raw, ok := documents.Bodies[ref]
		if !ok || len(raw) < 2 {
			return nil, false
		}
		sum := sha256.Sum256(raw)
		wire, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(wire, raw) || hex.EncodeToString(sum[:]) != ref.SHA256 || expected != "" && !bytes.Equal(raw, []byte(expected)) {
			return nil, false
		}
		return raw, true
	}
	raw, ok := read(inventory.BaseReceipt, inventory.BaseReceiptJCS)
	if !ok {
		return fail()
	}
	var base struct {
		Original  v2input.SessionVersionBinding `json:"original_source"`
		Processed v2input.SessionVersionBinding `json:"processed_source"`
		Recipe    v2input.Ref                   `json:"original_recipe"`
		Tenant    uuid.UUID                     `json:"tenant_id"`
		Parent    uuid.UUID                     `json:"parent_run_id"`
	}
	if json.Unmarshal(raw, &base) != nil || base.Tenant != source.Tenant || base.Parent != source.Parent || base.Recipe != source.OriginalRecipe || !authoredBindingValid(base.Original) || !authoredBindingValid(base.Processed) {
		return fail()
	}
	result := AuthoredCompoundContinuation{SchemaVersion: 2, Tenant: source.Tenant, Parent: source.Parent, Recipe: source.OriginalRecipe, OriginalSource: base.Original, ProgramSHA: source.ProgramSHA, BaseReceipt: inventory.BaseReceipt, Stages: []AuthoredGeometryOccurrence{}}
	predecessor := inventory.BaseReceipt.SHA256
	current := base.Processed
	registered := v2input.Ref{}
	for i, item := range inventory.Phases {
		raw, ok = read(item.Receipt, item.ReceiptJCS)
		if !ok || item.Ordinal != i+1 {
			return fail()
		}
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams(raw, &phase) != nil {
			return fail()
		}
		if phase.Ordinal != item.Ordinal || phase.Kind != item.Kind || phase.Instruction != item.Instruction || phase.Tenant != source.Tenant || phase.Parent != source.Parent || phase.ProgramSHA != source.ProgramSHA || phase.OriginalRecipe != source.OriginalRecipe || phase.InputSource != current || phase.PredecessorSHA != predecessor {
			return fail()
		}
		bound := source
		bound.Source = current
		bound.PredecessorSHA = predecessor
		bound.Instruction.Ordinal = phase.Ordinal
		bound.Instruction.Kind = phase.Kind
		bound.Instruction.Identity = phase.Instruction
		compiled, digest, err := CompileAuthoredPhaseReceipt(bound, phase.AuthoredPhaseTask, phase.OutputSource, phase.Result, phase.Report, phase.Geometry, phase.PhysicalClosure)
		if err != nil || digest != item.Receipt.SHA256 || !bytes.Equal(compiled, raw) {
			return fail()
		}
		switch phase.Kind {
		case "segment":
		case "geometry":
			compoundRaw, ok := read(phase.Result, "")
			if !ok || phase.Geometry == nil {
				return fail()
			}
			var compound struct {
				Schema      int                           `json:"schema_version"`
				Operation   string                        `json:"operation"`
				Version     string                        `json:"operation_version"`
				Tenant      uuid.UUID                     `json:"tenant_id"`
				Parent      uuid.UUID                     `json:"parent_run_id"`
				Instruction uuid.UUID                     `json:"instruction_id"`
				ProgramSHA  string                        `json:"program_sha256"`
				Recipe      v2input.Ref                   `json:"recipe"`
				Original    v2input.SessionVersionBinding `json:"original_source"`
				Input       v2input.SessionVersionBinding `json:"input_source"`
				Registered  v2input.Ref                   `json:"registered_original"`
				Image       v2input.Ref                   `json:"blended_image"`
				Native      struct {
					Run       uuid.UUID   `json:"run_id"`
					Task      uuid.UUID   `json:"task_id"`
					Result    uuid.UUID   `json:"result_id"`
					Attempt   int         `json:"attempt"`
					Fence     int64       `json:"fence"`
					Operation string      `json:"operation"`
					Version   string      `json:"operation_version"`
					Params    v2input.Ref `json:"params"`
					Root      v2input.Ref `json:"root"`
					Image     v2input.Ref `json:"image"`
					Report    v2input.Ref `json:"report"`
				} `json:"native"`
			}
			if json.Unmarshal(compoundRaw, &compound) != nil {
				return fail()
			}
			native := phase.Geometry
			if (compound.Schema != 1 && compound.Schema != 3) || compound.Operation != "render.authored_geometry" || compound.Version != "1" || compound.Tenant != source.Tenant || compound.Parent != source.Parent || compound.Instruction != phase.Instruction || compound.ProgramSHA != source.ProgramSHA || compound.Recipe != source.OriginalRecipe || compound.Original != base.Original || compound.Input != phase.InputSource || compound.Image != phase.OutputSource.Source || !validLayerArtifactRef(compound.Registered) {
				return fail()
			}
			n := compound.Native
			t := native.Task
			if n.Run != t.RunID || n.Task != t.TaskID || n.Result != t.ResultID || n.Attempt != t.Attempt || n.Fence != t.Fence || n.Operation != t.Operation || n.Version != t.Version || n.Params != t.Params || n.Root != native.Root || n.Image != native.Image || n.Report != native.Report {
				return fail()
			}
			if compound.Schema == 3 {
				prior := make([]v2input.Ref, 0, i)
				for _, p := range inventory.Phases[:i] {
					prior = append(prior, p.Receipt)
				}
				if verifyAuthoredFinalRoot3Mapping(documents, phase, compoundRaw, base.Original, prior) != nil {
					return fail()
				}
			}
			retained := false
			for _, ref := range phase.PhysicalClosure {
				if ref == compound.Registered {
					retained = true
				}
			}
			if !retained {
				return fail()
			}
			result.Stages = append(result.Stages, AuthoredGeometryOccurrence{Instruction: phase.Instruction, Phase: phase.Result, CanonicalReceipt: item.Receipt, InputSource: phase.InputSource})
			result.FinalImage = phase.OutputSource.Source
			registered = compound.Registered
		default:
			return fail()
		}
		current = phase.OutputSource
		predecessor = item.Receipt.SHA256
	}
	if len(result.Stages) < 1 || len(result.Stages) > 32 || current != source.Source || predecessor != source.PredecessorSHA {
		return fail()
	}
	body, err := canonical.Marshal(result)
	if err != nil {
		return fail()
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), registered, nil
}
