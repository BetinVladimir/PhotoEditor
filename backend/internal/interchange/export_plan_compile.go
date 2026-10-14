package interchange

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type ExportCrop = importCrop
type ExportLimits struct {
	ArchiveBytes, ExpandedBytes int64
	Files                       int
}

// Resolvers must prove the encoded content and portable recipe originate from
// this exact snapshot item, including its approved frozen render recipe.
type ExportFrame struct {
	ExternalID, Path        string
	Content, PortableRecipe v2input.Ref
	Rating                  int
	Pick                    string
	Crop                    *ExportCrop
	Label                   *string
}
type ExportFrameResolver interface {
	ResolveExportFrame(context.Context, v2input.SessionSnapshotItem) (ExportFrame, error)
}
type exportParamsDocument struct {
	Schema int         `json:"schema_version"`
	Plan   v2input.Ref `json:"plan"`
	Report int         `json:"max_report_bytes"`
}
type CompiledExportPlan struct {
	task                 uuid.UUID
	planRef, paramsRef   v2input.Ref
	planBody, paramsBody []byte
	roots                []v2input.Entry
}

func (p CompiledExportPlan) TaskID() uuid.UUID { return p.task }
func (p CompiledExportPlan) PlanDocument() (v2input.Ref, []byte) {
	return p.planRef, append([]byte(nil), p.planBody...)
}
func (p CompiledExportPlan) ParamsDocument() (v2input.Ref, []byte) {
	return p.paramsRef, append([]byte(nil), p.paramsBody...)
}
func (p CompiledExportPlan) InputRoots() []v2input.Entry {
	return append([]v2input.Entry(nil), p.roots...)
}

func CompileExportPlan(ctx context.Context, workflow, project uuid.UUID, format string, snapshotRef v2input.Ref, snapshotRaw []byte, limits ExportLimits, resolver ExportFrameResolver) (CompiledExportPlan, error) {
	var zero CompiledExportPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if workflow == uuid.Nil || project == uuid.Nil || !validImportRef(snapshotRef) || resolver == nil || limits.ArchiveBytes < 1 || limits.ArchiveBytes > 64<<30 || limits.ExpandedBytes < 1 || limits.ExpandedBytes > 128<<30 || limits.Files < 1 || limits.Files > 30000 {
		return zero, v2input.ErrInvalid
	}
	if format != "photoeditor_bundle" && format != "xmp" && format != "captureone_sidecar" {
		return zero, v2input.ErrInvalid
	}
	raw := append([]byte(nil), snapshotRaw...)
	if rawSHA(raw) != snapshotRef.SHA256 {
		return zero, v2input.ErrInvalid
	}
	var snapshot v2input.SessionInputSnapshot
	if err := decodeImportDocument(raw, &snapshot); err != nil {
		return zero, err
	}
	checked, expected, err := v2input.CompilePinnedSessionInputSnapshot(snapshot.SnapshotID, snapshot.Items)
	if err != nil || snapshot.SchemaVersion != 1 || !bytes.Equal(expected, raw) || checked.SnapshotDigest != snapshot.SnapshotDigest {
		return zero, v2input.ErrInvalid
	}
	task := uuid.NewSHA1(workflow, []byte("v2.interchange.export:0"))
	planID := uuid.NewSHA1(task, []byte("interchange:plan:1"))
	paramsID := uuid.NewSHA1(task, []byte("interchange:params:1"))
	plan := importPlanDocument{Schema: 1, Project: project, Direction: "export", Format: format, Snapshot: snapshot.SnapshotDigest, Archive: limits.ArchiveBytes, Expanded: limits.ExpandedBytes, Files: limits.Files, Entries: make([]importPlanEntry, 0, len(snapshot.Items))}
	roots := []v2input.Entry{}
	seen := map[uuid.UUID]v2input.Entry{}
	add := func(ref v2input.Ref, contract string) error {
		if !validImportRef(ref) {
			return v2input.ErrInvalid
		}
		entry := v2input.Entry{Ref: ref, Contract: contract}
		if old, ok := seen[ref.ArtifactID]; ok {
			if old != entry {
				return v2input.ErrInvalid
			}
			return nil
		}
		seen[ref.ArtifactID] = entry
		roots = append(roots, entry)
		return nil
	}
	if err := add(snapshotRef, "InputSnapshot"); err != nil {
		return zero, err
	}
	external, paths := map[string]bool{}, map[string]bool{}
	for _, item := range snapshot.Items {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		// Copy pointer fields before passing a snapshot item to external qualification.
		ownedRaw, err := canonical.Marshal(item)
		if err != nil {
			return zero, err
		}
		var owned v2input.SessionSnapshotItem
		if err := decodeImportDocument(ownedRaw, &owned); err != nil {
			return zero, err
		}
		frame, err := resolver.ResolveExportFrame(ctx, owned)
		if err != nil {
			return zero, err
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		folded := cases.Fold().String(frame.Path)
		if !utf8.ValidString(frame.ExternalID) || utf8.RuneCountInString(frame.ExternalID) < 1 || utf8.RuneCountInString(frame.ExternalID) > 240 || external[frame.ExternalID] || !validImportPlanPath(frame.Path) || paths[folded] || frame.Rating < -1 || frame.Rating > 5 || (frame.Pick != "picked" && frame.Pick != "rejected" && frame.Pick != "unmarked") || !validImportColorLabel(frame.Label) {
			return zero, v2input.ErrInvalid
		}
		external[frame.ExternalID] = true
		paths[folded] = true
		if crop := frame.Crop; crop != nil && (crop.X0 < 0 || crop.Y0 < 0 || crop.X1 > 1 || crop.Y1 > 1 || crop.X1 <= crop.X0 || crop.Y1 <= crop.Y0) {
			return zero, v2input.ErrInvalid
		}
		for _, entry := range []v2input.Entry{{Ref: item.WorkingImage, Contract: "WorkingImage"}, {Ref: item.Recipe, Contract: "FrozenRenderRecipe"}, {Ref: frame.Content, Contract: "EncodedImage"}, {Ref: frame.PortableRecipe, Contract: "EditingRecipeV3"}} {
			if err := add(entry.Ref, entry.Contract); err != nil {
				return zero, err
			}
		}
		binding, recipe := item.Binding, frame.PortableRecipe
		var crop *importCrop
		if frame.Crop != nil {
			copy := *frame.Crop
			crop = &copy
		}
		var label *string
		if frame.Label != nil {
			copy := *frame.Label
			label = &copy
		}
		plan.Entries = append(plan.Entries, importPlanEntry{ID: uuid.NewSHA1(workflow, []byte("interchange:entry:"+strconv.Itoa(item.Ordinal))), External: frame.ExternalID, Path: frame.Path, Content: frame.Content, Binding: &binding, Recipe: &recipe, Rating: frame.Rating, Pick: frame.Pick, Crop: crop, Label: label})
	}
	sort.Slice(plan.Entries, func(i, j int) bool { return plan.Entries[i].Path < plan.Entries[j].Path })
	if len(plan.Entries)*2+1 > limits.Files {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(plan)
	if err != nil || len(body) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	planRef := v2input.Ref{ArtifactID: planID, SHA256: rawSHA(body)}
	params, err := canonical.Marshal(exportParamsDocument{Schema: 1, Plan: planRef, Report: 8 << 20})
	if err != nil {
		return zero, err
	}
	paramsRef := v2input.Ref{ArtifactID: paramsID, SHA256: rawSHA(params)}
	if err := add(planRef, "InterchangePlan"); err != nil {
		return zero, err
	}
	if err := add(paramsRef, "Params"); err != nil {
		return zero, err
	}
	return CompiledExportPlan{task: task, planRef: planRef, paramsRef: paramsRef, planBody: body, paramsBody: params, roots: roots}, ctx.Err()
}
