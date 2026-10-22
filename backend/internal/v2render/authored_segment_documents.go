package v2render

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// AuthoredSegmentDocuments contains actual immutable object bytes for the SQL
// selected transcript. It is not an admission or an adoption receipt. Typed
// recursive expansion and independent native replay remain mandatory downstream.
type AuthoredSegmentDocuments struct {
	Inventory AuthoredSegmentInventory
	Bodies    map[v2input.Ref][]byte
}

// ReadAuthoredSegmentDocuments never takes replacement refs or receipt bodies.
// It reads the base and every phase document from the exact current transcript,
// verifies the physical bytes against that transcript, then repeats the entire
// current authority after IO. A caller cannot continue with a partial inventory.
func ReadAuthoredSegmentDocuments(ctx context.Context, scheduler *pgxpool.Pool, objects SQLAuthoredPhaseObjects, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (AuthoredSegmentDocuments, bool, error) {
	empty := AuthoredSegmentDocuments{}
	inventory, found, err := ReadAuthoredSegmentInventory(ctx, scheduler, tenant, parent, ordinal)
	if err != nil || !found {
		return empty, found, err
	}
	if who.UserID != inventory.Context.Actor || who.SessionID != inventory.Context.Session {
		return empty, false, v2input.ErrInvalid
	}
	out := AuthoredSegmentDocuments{Inventory: inventory, Bodies: make(map[v2input.Ref][]byte)}
	expected := map[v2input.Ref]string{inventory.BaseReceipt: inventory.BaseReceiptJCS, inventory.Context.OriginalRecipe: inventory.Context.RecipeJCS}
	for _, phase := range inventory.Phases {
		if prior, ok := expected[phase.Receipt]; ok && prior != phase.ReceiptJCS {
			return empty, false, v2input.ErrInvalid
		}
		expected[phase.Receipt] = phase.ReceiptJCS
	}
	// Check artifact identity independently of digest to forbid conflicting refs.
	ids := map[uuid.UUID]v2input.Ref{}
	for ref, body := range expected {
		if prior, ok := ids[ref.ArtifactID]; ok && prior != ref {
			return empty, false, v2input.ErrInvalid
		}
		ids[ref.ArtifactID] = ref
		raw, e := objects.ReadJSON(ctx, who, tenant, inventory.Context.Project, parent, ordinal, ref)
		if e != nil {
			return empty, false, e
		}
		if !bytes.Equal(raw, []byte(body)) {
			return empty, false, v2input.ErrInvalid
		}
		out.Bodies[ref] = append([]byte(nil), raw...)
	}
	for _, phase := range inventory.Phases {
		var receipt AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(phase.ReceiptJCS), &receipt) != nil {
			return empty, false, v2input.ErrInvalid
		}
		// These JSON roots are actual completed producer occurrences, not future
		// outputs. Exact operation-specific parsers consume these bytes next.
		roots := []v2input.Ref{receipt.Result, receipt.Report, receipt.OutputSource.Source}
		if receipt.Geometry != nil {
			roots = append(roots, receipt.Geometry.Root, receipt.Geometry.Report, receipt.Geometry.Image)
		}
		for _, ref := range roots {
			if prior, ok := ids[ref.ArtifactID]; ok && prior != ref {
				return empty, false, v2input.ErrInvalid
			}
			ids[ref.ArtifactID] = ref
			if _, ok := out.Bodies[ref]; ok {
				continue
			}
			raw, e := objects.ReadJSON(ctx, who, tenant, inventory.Context.Project, parent, ordinal, ref)
			if e != nil {
				return empty, false, e
			}
			var document map[string]json.RawMessage
			wire, e := canonical.Bytes(raw)
			if e != nil || !bytes.Equal(wire, raw) || json.Unmarshal(raw, &document) != nil || len(document) == 0 {
				return empty, false, v2input.ErrInvalid
			}
			out.Bodies[ref] = append([]byte(nil), raw...)
		}
	}
	after, found, err := ReadAuthoredSegmentInventory(ctx, scheduler, tenant, parent, ordinal)
	if err != nil || !found {
		return empty, false, err
	}
	beforeWire, err := canonical.Marshal(inventory)
	if err != nil {
		return empty, false, err
	}
	afterWire, err := canonical.Marshal(after)
	if err != nil {
		return empty, false, err
	}
	if !bytes.Equal(beforeWire, afterWire) {
		return empty, false, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return empty, false, err
	}
	return out, true, nil
}
