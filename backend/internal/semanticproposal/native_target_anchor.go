package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// An owned target binding retains the original intent, rather than manufacturing
// another public proposal with a substituted reference. Persisted SQL authority
// and target source/model/Params locks are still mandatory before draft creation.
type nativeTargetAnchor struct {
	request      Request
	key          string
	published    PublishedIntent
	snapshot     v2input.Ref
	snapshotBody json.RawMessage
	manifest     nativeTargetPreparationManifest
	entry        nativeTargetPreparationEntry
	stage        nativeTargetPreparationStage
	seal         string
}

func compileNativeTargetAnchor(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody json.RawMessage, manifest nativeTargetPreparationManifest, ordinal int, operation string) (nativeTargetAnchor, error) {
	var zero nativeTargetAnchor
	saved, err := restoreNativeTargetPreparationManifest(ctx, who, request, key, published, snapshot, snapshotBody, manifest.body, manifest.sha)
	if err != nil {
		return zero, err
	}
	var body nativeTargetPreparationManifestBody
	if json.Unmarshal(saved.body, &body) != nil || ordinal < 0 || ordinal >= len(body.Entries) {
		return zero, v2input.ErrInvalid
	}
	entry := body.Entries[ordinal]
	if entry.ReuseReference {
		return zero, v2input.ErrConflict
	}
	for _, stage := range entry.Stages {
		if stage.Operation != operation {
			continue
		}
		owned, err := ownNativePlanRequest(ctx, who, request, key)
		if err != nil {
			return zero, err
		}
		inventory := CatalogInventory{Scope: published.Inventory.Scope, Targets: append([]v2input.SessionVersionBinding(nil), published.Inventory.Targets...)}
		anchor := nativeTargetAnchor{request: owned, key: key, published: PublishedIntent{Ref: published.Ref, Inventory: inventory}, snapshot: snapshot, snapshotBody: append(json.RawMessage(nil), snapshotBody...), manifest: saved, entry: entry, stage: stage}
		anchor.seal, err = targetAnchorDigest(anchor)
		if err != nil {
			return zero, err
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return anchor, nil
	}
	return zero, v2input.ErrInvalid
}

func targetAnchorDigest(a nativeTargetAnchor) (string, error) {
	return canonical.Hash(map[string]any{"request": a.request, "key": a.key, "published": a.published, "snapshot": a.snapshot, "snapshot_body": a.snapshotBody, "manifest_body": a.manifest.body, "manifest_sha": a.manifest.sha, "entry": a.entry, "stage": a.stage})
}
func verifyNativeTargetAnchor(ctx context.Context, a nativeTargetAnchor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := targetAnchorDigest(a)
	if err != nil {
		return err
	}
	if digest != a.seal {
		return v2input.ErrConflict
	}
	return nil
}
