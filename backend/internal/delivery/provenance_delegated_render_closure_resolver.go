package delivery

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// DelegatedRenderInputObjectCatalog is scoped to one adopted render task and
// its exact registered inventory. The closure walker supplies the typed path.
type DelegatedRenderInputObjectCatalog struct {
	Tasks SQLDelegatedTaskCatalog
	Task provenance.CanonicalSigningTask
	Source v2input.Ref
	SourceVersion uuid.UUID
	Inventory []v2input.Entry
}

func (s DelegatedRenderInputObjectCatalog) Lookup(ctx context.Context, _ auth.Identity,
	tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil { return zero, err }
	if s.Tasks.DB == nil || tenant != s.Tasks.Scope.TenantID ||
		s.SourceVersion == uuid.Nil ||
		verifyDelegatedRenderInputInventory(s.Task, s.Source, s.Inventory) != nil ||
		node.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(node.Ref.SHA256) {
		return zero, v2input.ErrInvalid
	}
	contract := node.Contract
	if contract == "WorkingPixels" || contract == "WorkingAlpha" ||
		contract == "MaskPixels" || contract == "DenseWarpField" { contract = "NpyArray" }
	registeredContract, valid := delegatedRegisteredOccurrence(s.Inventory, node, contract)
	if !valid { return zero, v2input.ErrInvalid }
	if node.ParentRef.ArtifactID == uuid.Nil &&
		!(node.Ref == s.Task.Params && contract == "FrozenRenderRecipe") &&
		!(node.Ref == s.Source && contract == "WorkingImage") {
		return zero, v2input.ErrInvalid
	}
	if query := delegatedRenderLiveGuard(registeredContract); query != "" {
		var live bool
		err := s.Tasks.DB.QueryRow(ctx, query, s.Tasks.Lease.Expansion,
			s.Tasks.Lease.Token, s.Tasks.Slot.Ordinal, s.Tasks.Slot.PresetID,
			s.Task.Task, node.Ref.ArtifactID, node.Ref.SHA256).Scan(&live)
		if err != nil { return zero, err }
		if !live { return zero, v2input.ErrInvalid }
	}
	if node.Ref == s.Source && registeredContract == "WorkingImage" {
		var live bool
		err := s.Tasks.DB.QueryRow(ctx,
			`SELECT photo.v31_delivery_render_source_live($1,$2,$3,$4,$5,$6,$7)`,
			s.Tasks.Lease.Expansion, s.Tasks.Lease.Token,
			s.Tasks.Slot.Ordinal, s.Tasks.Slot.PresetID, s.Task.Task,
			node.Ref.ArtifactID, node.Ref.SHA256).Scan(&live)
		if err != nil { return zero, err }
		if !live { return zero, v2input.ErrInvalid }
	}
	if node.RenderFrame == nil && node.ParentRef == s.Source &&
		(node.Contract == "WorkingPixels" || node.Contract == "WorkingAlpha") {
		var live bool
		err := s.Tasks.DB.QueryRow(ctx,
			`SELECT photo.v31_delivery_render_source_array_live(
			 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, s.Tasks.Lease.Expansion,
			s.Tasks.Lease.Token, s.Tasks.Slot.Ordinal, s.Tasks.Slot.PresetID,
			s.Task.Task, node.ParentRef.ArtifactID, node.ParentRef.SHA256,
			node.Ref.ArtifactID, node.Ref.SHA256, node.Contract).Scan(&live)
		if err != nil { return zero, err }
		if !live { return zero, v2input.ErrInvalid }
	}
	if node.RenderFrame != nil {
		frame := node.RenderFrame
		if node.ExpectedBinding == nil ||
			frame.Root != s.Task.Params || frame.Descriptor != frame.Learning.Descriptor ||
			frame.SelectedVersion != s.SourceVersion ||
			node.ExpectedBinding.AssetVersionID == uuid.Nil ||
			frame.Ordinal < 0 || frame.Ordinal > 4999 ||
			frame.Learning.AnalysisID == uuid.Nil ||
			!shaPattern.MatchString(frame.ModelLockSHA) {
			return zero, v2input.ErrInvalid
		}
		args := []any{s.Tasks.Lease.Expansion, s.Tasks.Lease.Token,
			s.Tasks.Slot.Ordinal, s.Tasks.Slot.PresetID, s.Task.Task,
			frame.Ordinal, node.ExpectedBinding.AssetVersionID}
		query := ""
		switch node.Contract {
		case "WorkingImage":
			if node.ExpectedBinding.Source != node.Ref { return zero, v2input.ErrInvalid }
			query = `SELECT photo.v31_delivery_render_frame_source_live($1,$2,$3,$4,$5,$6,$7,$8,$9)`
			args = append(args, node.Ref.ArtifactID, node.Ref.SHA256)
		case "FaceObservations":
			if node.ParentRef != frame.Descriptor { return zero, v2input.ErrInvalid }
			query = `SELECT photo.v31_delivery_render_frame_faces_live($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
			args = append(args, node.ExpectedBinding.Source.ArtifactID,
				node.ExpectedBinding.Source.SHA256, node.Ref.ArtifactID, node.Ref.SHA256)
		case "WorkingPixels", "WorkingAlpha":
			if node.ParentRef != node.ExpectedBinding.Source { return zero, v2input.ErrInvalid }
			query = `SELECT photo.v31_delivery_render_frame_array_live($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
			args = append(args, node.ParentRef.ArtifactID, node.ParentRef.SHA256,
				node.Ref.ArtifactID, node.Ref.SHA256, node.Contract)
		default:
			return zero, v2input.ErrInvalid
		}
		var live bool
		err := s.Tasks.DB.QueryRow(ctx, query, args...).Scan(&live)
		if err != nil { return zero, err }
		if !live { return zero, v2input.ErrInvalid }
	}
	var raw []byte
	err := s.Tasks.DB.QueryRow(ctx, `SELECT photo.v31_delivery_render_input_object(
	 $1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tasks.Lease.Expansion,
		s.Tasks.Lease.Token, s.Tasks.Slot.Ordinal, s.Tasks.Slot.PresetID,
		s.Task.Task, node.Ref.ArtifactID, node.Ref.SHA256, registeredContract,
		s.Tasks.Bucket).Scan(&raw)
	if err != nil { return zero, err }
	var row struct {
		Bucket string `json:"bucket"`
		Key string `json:"key"`
		Version string `json:"version"`
		SHA256 string `json:"sha256"`
		Bytes int64 `json:"bytes"`
		MediaType string `json:"media_type"`
	}
	if len(raw) < 2 || len(raw) > 4096 || json.Unmarshal(raw, &row) != nil ||
		row.Bucket != s.Tasks.Bucket || row.Key == "" || len(row.Key) > 2048 ||
		row.Version == "" || row.Version == "null" || len(row.Version) > 1024 ||
		row.SHA256 != node.Ref.SHA256 || row.Bytes < 1 || row.Bytes > 1<<36 ||
		row.MediaType == "" || len(row.MediaType) > 120 { return zero, v2input.ErrInvalid }
	return objectstore.Object{Key: row.Key, VersionID: row.Version, SHA256: row.SHA256,
		Bytes: row.Bytes, ContentType: row.MediaType}, ctx.Err()
}

func delegatedRenderLiveGuard(contract string) string {
	switch contract {
	case "CandidateProfile":
		return `SELECT photo.v31_delivery_render_model_live($1,$2,$3,$4,$5,$6,$7)`
	case "DescriptorShard":
		return `SELECT photo.v31_delivery_render_descriptor_live($1,$2,$3,$4,$5,$6,$7)`
	case "StyleBackgroundAsset":
		return `SELECT photo.v31_delivery_render_background_live($1,$2,$3,$4,$5,$6,$7)`
	default:
		return ""
	}
}

func delegatedRegisteredOccurrence(inventory []v2input.Entry,
	node v2draft.LayerRef, contract string) (string, bool) {
	parentFound := node.ParentRef.ArtifactID == uuid.Nil
	registered := ""
	for _, entry := range inventory {
		if entry.Ref == node.ParentRef { parentFound = true }
		if entry.Ref == node.Ref &&
			(entry.Contract == contract || node.Contract == "Artifact") {
			registered = entry.Contract
		}
	}
	return registered, registered != "" && parentFound
}

var _ v2draft.ArtifactCatalog = DelegatedRenderInputObjectCatalog{}

// DelegatedRenderClosureResolver reuses the editor's typed closure walker,
// replacing its session catalog with the parent-lease-scoped task inventory.
type DelegatedRenderClosureResolver struct {
	Inputs DelegatedRenderInputCatalog
	Objects v2input.SessionAnalysisObjectReader
}

func (s DelegatedRenderClosureResolver) ResolveDelegatedTaskInputEvidence(ctx context.Context,
	task provenance.CanonicalSigningTask, roots []v2input.Entry,
	binding v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error) {
	var zero v2draft.TaskInputEvidence
	if err := ctx.Err(); err != nil { return zero, err }
	if s.Objects == nil || len(roots) != 2 ||
		roots[0] != (v2input.Entry{Ref: binding.Source, Contract: "WorkingImage"}) ||
		roots[1] != (v2input.Entry{Ref: task.Params, Contract: "FrozenRenderRecipe"}) {
		return zero, v2input.ErrInvalid
	}
	inventory, err := s.Inputs.ReadDelegatedRenderInputs(ctx, task, binding.Source)
	if err != nil { return zero, err }
	resolver := v2draft.ClosureResolver{Catalog: DelegatedRenderInputObjectCatalog{
		Tasks: s.Inputs.Tasks, Task: task, Source: binding.Source,
		SourceVersion: binding.AssetVersionID,
		Inventory: inventory}, Objects: s.Objects}
	evidence, err := resolver.ResolveTaskInputEvidence(ctx, auth.Identity{},
		s.Inputs.Tasks.Scope.TenantID, roots, binding)
	if err != nil { return zero, err }
	recipe, err := provenance.ReadDelegatedRenderRecipe(ctx,
		DelegatedTaskDocumentReader{Tasks: s.Inputs.Tasks, Objects: s.Objects}, task)
	if err != nil { return zero, err }
	if recipe.Scalars.Source != binding { return zero, v2input.ErrInvalid }
	if err := provenance.VerifyDelegatedRenderInputBytes(ctx, resolver.Catalog,
		s.Objects, s.Inputs.Tasks.Scope.TenantID, evidence,
		recipe.Scalars.ICCLockSHA); err != nil { return zero, err }
	computed, err := canonical.Marshal(evidence.Entries)
	if err != nil { return zero, err }
	registered, err := canonical.Marshal(inventory)
	if err != nil || !bytes.Equal(computed, registered) { return zero, v2input.ErrInvalid }
	current, err := s.Inputs.ReadDelegatedRenderInputs(ctx, task, binding.Source)
	if err != nil { return zero, err }
	repeated, err := canonical.Marshal(current)
	if err != nil || !bytes.Equal(registered, repeated) { return zero, v2input.ErrInvalid }
	return evidence, ctx.Err()
}

var _ provenance.DelegatedRenderClosureResolver = DelegatedRenderClosureResolver{}
