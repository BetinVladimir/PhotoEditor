package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// LensProfileChoice is keyed by the selected version, never by request order.
// ProfileJSON must come from the authorized immutable profile catalog; its
// qualification ref is retained in the task closure and checked at activation.
type LensProfileChoice struct {
	AssetVersionID uuid.UUID
	Strength       float64
	Profile        Ref
	ProfileJSON    json.RawMessage
}

type LensAdmissionLimits struct{ RAMBytes, ScratchBytes, OutputBytes int64 }

// CompileLensSelection constructs all params/original documents and validates
// resource estimates before draft admission performs any writes. Provisional
// document IDs are deterministic compiler identities, replaced by the scoped
// document publisher before final task staging; their SHA fixes exact content.
func CompileLensSelection(workflow uuid.UUID, sources []LensSelectedSource, choices []LensProfileChoice, limits LensAdmissionLimits) ([]LensTaskRequest, error) {
	if workflow == uuid.Nil || len(sources) < 1 || len(sources) > 5000 || len(choices) != len(sources) || limits.RAMBytes < 1 || limits.ScratchBytes < 1 || limits.OutputBytes < 1 {
		return nil, ErrInvalid
	}
	selected := make(map[uuid.UUID]LensProfileChoice, len(choices))
	for _, choice := range choices {
		if choice.AssetVersionID == uuid.Nil || !finiteRange(choice.Strength, 0, 1) || !validRef(choice.Profile) {
			return nil, ErrInvalid
		}
		if _, duplicate := selected[choice.AssetVersionID]; duplicate {
			return nil, ErrInvalid
		}
		selected[choice.AssetVersionID] = choice
	}
	requests := make([]LensTaskRequest, 0, len(sources))
	seen := map[uuid.UUID]bool{}
	for ordinal, source := range sources {
		if source.Ordinal != ordinal || source.Binding.AssetVersionID == uuid.Nil || seen[source.Binding.AssetVersionID] || source.Original.Binding != source.Binding {
			return nil, ErrInvalid
		}
		seen[source.Binding.AssetVersionID] = true
		choice, exists := selected[source.Binding.AssetVersionID]
		if !exists {
			return nil, ErrInvalid
		}
		profile, err := ParseLensProfile(choice.ProfileJSON)
		if err != nil {
			return nil, err
		}
		params := OpticalLensParams{SchemaVersion: 1, Base: source.Binding, Strength: choice.Strength, Profile: choice.Profile, FocalMM: profile.FocalMM, Aperture: profile.Aperture}
		paramsSHA, err := canonical.Hash(params)
		if err != nil {
			return nil, err
		}
		originalSHA, err := canonical.Hash(source.Original)
		if err != nil {
			return nil, err
		}
		task, err := TaskID(workflow, "optical.lens", ordinal)
		if err != nil {
			return nil, err
		}
		request := LensTaskRequest{Workflow: workflow, Ordinal: ordinal, Params: params, ParamsRef: Ref{uuid.NewSHA1(task, []byte("v25_lens_params")), paramsSHA}, Original: source.Original, OriginalRef: Ref{uuid.NewSHA1(task, []byte("v25_lens_original_binding")), originalSHA}, ProfileJSON: append(json.RawMessage(nil), choice.ProfileJSON...), SourceJSON: append(json.RawMessage(nil), source.SourceJSON...), OriginalBytes: source.OriginalBytes, RAMLimit: limits.RAMBytes, ScratchLimit: limits.ScratchBytes, OutputLimit: limits.OutputBytes}
		if _, err = CompileLensTask(request); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}
