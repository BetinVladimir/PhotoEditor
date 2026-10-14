package interchange

import (
	"context"
	"crypto/ed25519"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type ExportPersonOriginPublished struct {
	Scope      PortablePersonOriginExpectation
	Attachment PreparedPersonOriginAttachment
}

type ExportPersonOriginInputPublisher interface {
	Publish(context.Context, PortablePersonOriginExpectation, PreparedPersonOriginAttachment) (v2input.Ref, error)
}

// PublishExportPersonOriginInputs completes physical publication before the
// export task digest is materialized. Only signed, current, source-bound
// receipts and their original MaskRef documents extend the typed inventory.
func PublishExportPersonOriginInputs(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments,
	base ExportInputInventory, candidates []ExportPersonOriginPublished,
	publisher ExportPersonOriginInputPublisher, trusted map[PortablePersonOriginKey]ed25519.PublicKey,
	current ExportPersonOriginCurrentChecker) (ExportInputInventory, map[string]PortablePersonOriginExpectation, error) {
	var zero ExportInputInventory
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	if publisher == nil || current == nil || len(trusted) == 0 || len(candidates) > 5000 {
		return zero, nil, v2input.ErrInvalid
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return zero, nil, err
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		return zero, nil, err
	}
	if plan.Direction != "export" || plan.Format != "photoeditor_bundle" || base.task != compiled.task ||
		len(base.entries) == 0 || len(base.entries)+2*len(candidates) > v2input.MaxEntries {
		return zero, nil, v2input.ErrInvalid
	}
	baseDigest, err := canonical.Hash(base.entries)
	if err != nil || baseDigest != base.digest {
		return zero, nil, v2input.ErrInvalid
	}
	selected := make(map[uuid.UUID]importPlanEntry, len(plan.Entries))
	for _, entry := range plan.Entries {
		if _, duplicate := selected[entry.ID]; duplicate {
			return zero, nil, v2input.ErrInvalid
		}
		selected[entry.ID] = entry
	}
	entries := base.Entries()
	byID := make(map[uuid.UUID]v2input.Entry, len(entries)+2*len(candidates))
	for _, entry := range entries {
		if !validImportRef(entry.Ref) || byID[entry.Ref.ArtifactID].Contract != "" {
			return zero, nil, v2input.ErrInvalid
		}
		byID[entry.Ref.ArtifactID] = entry
	}
	for _, root := range compiled.roots {
		if byID[root.Ref.ArtifactID] != root {
			return zero, nil, v2input.ErrInvalid
		}
	}
	expected := make(map[string]PortablePersonOriginExpectation, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return zero, nil, err
		}
		scope, attachment := candidate.Scope, candidate.Attachment
		entry, exists := selected[scope.EntryID]
		if !exists || entry.Binding == nil || *entry.Binding != scope.Source ||
			entry.Recipe == nil || *entry.Recipe != scope.Recipe ||
			!portablePersonOriginSHA.MatchString(scope.ModelLockSHA) ||
			attachment.Path != "origins/"+scope.EntryID.String()+"/"+scope.MaskRef.ArtifactID.String()+".json" ||
			expected[attachment.Path].EntryID != uuid.Nil ||
			attachment.Ref.ArtifactID != uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())) ||
			attachment.Ref.SHA256 != rawSHA(attachment.Body) ||
			attachment.Mapping.EntryID != scope.EntryID || attachment.Mapping.Recipe != scope.Recipe ||
			attachment.Mapping.Override != scope.Override || attachment.Mapping.MaskRef != scope.MaskRef ||
			attachment.Mapping.MaskPixels != scope.MaskPixels || attachment.Mapping.Path != attachment.Path ||
			attachment.Mapping.SHA != attachment.Ref.SHA256 {
			return zero, nil, v2input.ErrInvalid
		}
		for _, needed := range []v2input.Entry{{Ref: scope.Recipe, Contract: "EditingRecipeV3"},
			{Ref: scope.Override, Contract: "Override"}, {Ref: scope.MaskPixels, Contract: "MaskPixels"}} {
			if byID[needed.Ref.ArtifactID] != needed {
				return zero, nil, v2input.ErrInvalid
			}
		}
		claim, err := VerifyPortablePersonOrigin(attachment.Body, scope, trusted)
		if err != nil {
			return zero, nil, err
		}
		if err := current.RecheckExportPersonOrigin(ctx, claim); err != nil {
			return zero, nil, err
		}
		mask := v2input.Entry{Ref: scope.MaskRef, Contract: "MaskRef"}
		if old, present := byID[mask.Ref.ArtifactID]; present {
			if old != mask {
				return zero, nil, v2input.ErrInvalid
			}
		} else {
			entries = append(entries, mask)
			byID[mask.Ref.ArtifactID] = mask
		}
		receipt := v2input.Entry{Ref: attachment.Ref, Contract: "PersonOriginReceipt"}
		if _, present := byID[receipt.Ref.ArtifactID]; present {
			return zero, nil, v2input.ErrInvalid
		}
		published, err := publisher.Publish(ctx, scope, attachment)
		if err != nil {
			return zero, nil, err
		}
		if published != receipt.Ref {
			return zero, nil, v2input.ErrInvalid
		}
		if err := current.RecheckExportPersonOrigin(ctx, claim); err != nil {
			return zero, nil, err
		}
		entries = append(entries, receipt)
		byID[receipt.Ref.ArtifactID] = receipt
		expected[attachment.Path] = scope
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String()
	})
	digest, err := canonical.Hash(entries)
	if err != nil {
		return zero, nil, err
	}
	return ExportInputInventory{task: base.task, entries: entries, digest: digest,
			backgrounds: append([]bundleBackground(nil), base.backgrounds...), backgroundDigest: base.backgroundDigest},
		expected, ctx.Err()
}
