package delivery

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// CreateRecipeInputResolver must authorize and traverse both exact roots and
// every reachable dependency. A list assembled from artifact metadata alone is
// insufficient. Final SQL admission must repeat live source authority.
type CreateRecipeInputResolver interface {
	ResolveCreateRecipeInputs(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2input.SessionSnapshotItem) ([]v2input.Entry, error)
}

type SQLCreateRecipeInputResolver struct {
	DB      *pgxpool.Pool
	Bucket  string
	Objects v2input.SessionAnalysisObjectReader
}

func (r SQLCreateRecipeInputResolver) ResolveCreateRecipeInputs(ctx context.Context, who auth.Identity, tid, project uuid.UUID, item v2input.SessionSnapshotItem) ([]v2input.Entry, error) {
	resolver := v2draft.ClosureResolver{
		Catalog: v2draft.SQLArtifactCatalog{DB: r.DB, Bucket: r.Bucket, ProjectID: project, AssetVersionID: item.Binding.AssetVersionID},
		Objects: r.Objects,
	}
	evidence, err := resolver.ResolveTaskInputEvidence(ctx, who, tid, []v2input.Entry{
		{Ref: item.WorkingImage, Contract: "WorkingImage"},
		{Ref: item.Recipe, Contract: "FrozenRenderRecipe"},
	}, item.Binding)
	if err != nil {
		return nil, err
	}
	// Traversal reads documents outside transactions. Recheck every scoped
	// occurrence, including repeated refs and binary leaves, after those reads.
	// This detects revocation or changed physical versions during traversal;
	// final registration still requires an independent transaction fence.
	if err := recheckCreateRecipeOccurrences(ctx, who, tid, resolver.Catalog, evidence.Occurrences); err != nil {
		return nil, err
	}
	return append([]v2input.Entry(nil), evidence.Entries...), nil
}

func recheckCreateRecipeOccurrences(ctx context.Context, who auth.Identity, tid uuid.UUID, catalog v2draft.ArtifactCatalog, occurrences []v2draft.OccurrenceWitness) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if catalog == nil || len(occurrences) < 2 {
		return v2input.ErrInvalid
	}
	for _, occurrence := range occurrences {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := catalog.Lookup(ctx, who, tid, occurrence.Node)
		if err != nil {
			return err
		}
		if current != occurrence.Object {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

// ResolvePrepareTask resolves each source under its own version scope, merges
// exact shared dependencies, and pins an owned closure. Publication of the
// locally compiled creation documents and SQL registration are separate steps.
func (i CreateIntent) ResolvePrepareTask(ctx context.Context, who auth.Identity, pendingRaw []byte, resolver CreateRecipeInputResolver, limits PrepareResourceLimits) (FrozenPrepareTask, error) {
 return i.resolvePrepareTask(ctx,who,pendingRaw,resolver,limits,nil,false)
}
func(i CreateIntent)ResolveWatermarkPrepareTask(ctx context.Context,who auth.Identity,pendingRaw []byte,resolver CreateRecipeInputResolver,limits PrepareResourceLimits,pins []PreparedWatermarkPin)(FrozenPrepareTask,error){return i.resolvePrepareTask(ctx,who,pendingRaw,resolver,limits,pins,true)}
func(i CreateIntent)resolvePrepareTask(ctx context.Context,who auth.Identity,pendingRaw []byte,resolver CreateRecipeInputResolver,limits PrepareResourceLimits,pins []PreparedWatermarkPin,watermark bool)(FrozenPrepareTask,error){
	if err := ctx.Err(); err != nil {
		return FrozenPrepareTask{}, err
	}
	if resolver == nil || who.UserID != i.actor || who.SessionID == uuid.Nil {
		return FrozenPrepareTask{}, v2input.ErrInvalid
	}
	pending, err := i.decodePending(ctx, who.SessionID, pendingRaw,pins,watermark)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	docs, err := i.compileDocuments(ctx, who.SessionID, pending.Catalog, pending.Items,pins,watermark)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	merged := make(map[uuid.UUID]v2input.Entry)
	add := func(entry v2input.Entry) error {
		if entry.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(entry.Ref.SHA256) || len(entry.Contract) < 1 || len(entry.Contract) > 120 || entry.Contract == "Params" || entry.Contract == "InputClosureManifest" || entry.Contract == "InputClosureChunk" {
			return v2input.ErrInvalid
		}
		if prior, exists := merged[entry.Ref.ArtifactID]; exists && prior != entry {
			return v2input.ErrInvalid
		}
		merged[entry.Ref.ArtifactID] = entry
		if len(merged) > 999999 {
			return v2input.ErrInvalid
		}
		return nil
	}
	for _, item := range pending.Items {
		if err := ctx.Err(); err != nil {
			return FrozenPrepareTask{}, err
		}
		entries, err := resolver.ResolveCreateRecipeInputs(ctx, who, i.tenant, i.request.ProjectID, item)
		if err != nil {
			return FrozenPrepareTask{}, err
		}
		if len(entries) < 2 || len(entries) > 4096 {
			return FrozenPrepareTask{}, v2input.ErrInvalid
		}
		foundSource, foundRecipe := false, false
		for _, entry := range entries {
			if err := add(entry); err != nil {
				return FrozenPrepareTask{}, err
			}
			foundSource = foundSource || entry == (v2input.Entry{Ref: item.WorkingImage, Contract: "WorkingImage"})
			foundRecipe = foundRecipe || entry == (v2input.Entry{Ref: item.Recipe, Contract: "FrozenRenderRecipe"})
		}
		if !foundSource || !foundRecipe {
			return FrozenPrepareTask{}, v2input.ErrInvalid
		}
	}
	for _, root := range docs.Plan.Roots() {
		if err := add(root); err != nil {
			return FrozenPrepareTask{}, err
		}
	}
	entries := make([]v2input.Entry, 0, len(merged))
	for _, entry := range merged {
		entries = append(entries, entry)
	}
	if err := ctx.Err(); err != nil {
		return FrozenPrepareTask{}, err
	}
	return docs.Plan.FreezeTask(entries, limits)
}
