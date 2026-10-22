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

// AuthoredGeometryDocuments contains actual immutable object bytes for the SQL
// selected transcript. It is not an admission or an adoption receipt. Typed
// recursive expansion and independent native replay remain mandatory downstream.
type AuthoredGeometryDocuments struct {
	Inventory AuthoredGeometryPrefix
	Bodies    map[v2input.Ref][]byte
}

// ReadAuthoredGeometryDocuments never takes replacement refs or receipt bodies.
// It reads the base and every phase document from the exact current transcript,
// verifies the physical bytes against that transcript, then repeats the entire
// current authority after IO. A caller cannot continue with a partial inventory.
func ReadAuthoredGeometryDocuments(ctx context.Context, scheduler *pgxpool.Pool, objects SQLAuthoredGeometryPhaseObjects, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (AuthoredGeometryDocuments, bool, error) {
	empty := AuthoredGeometryDocuments{}
	inventory, found, err := ReadAuthoredGeometryPrefix(ctx, scheduler, tenant, parent, ordinal)
	if err != nil || !found {
		return empty, found, err
	}
	if who.UserID != inventory.Context.Actor || who.SessionID != inventory.Context.Session {
		return empty, false, v2input.ErrInvalid
	}
	out := AuthoredGeometryDocuments{Inventory: inventory, Bodies: make(map[v2input.Ref][]byte)}
	totalBytes := 0
	store := func(ref v2input.Ref, raw []byte) error {
		if _, exists := out.Bodies[ref]; exists {
			return v2input.ErrInvalid
		}
		if len(raw) < 2 || len(raw) > 8<<20 || len(raw) > (32<<20)-totalBytes || len(out.Bodies) >= 4096 {
			return v2input.ErrInvalid
		}
		out.Bodies[ref] = append([]byte(nil), raw...)
		totalBytes += len(raw)
		return nil
	}
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
		if err := store(ref, raw); err != nil {
			return empty, false, err
		}
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
			if err := store(ref, raw); err != nil {
				return empty, false, err
			}
		}
	}
	// Root3 has an explicit typed mapping; read its real output documents,
	// never derive a correspondence from the physical_closure inventory alone.
	for _, item := range inventory.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if phase.Kind != "geometry" {
			continue
		}
		var root struct {
			Schema     int         `json:"schema_version"`
			Mapping    v2input.Ref `json:"mask_mapping"`
			Registered v2input.Ref `json:"registered_original"`
			Blend      struct {
				Baseline v2input.Ref `json:"baseline"`
			} `json:"blend"`
		}
		if json.Unmarshal(out.Bodies[phase.Result], &root) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if root.Schema != 3 {
			continue
		}
		queue := []v2input.Ref{root.Mapping, root.Registered, root.Blend.Baseline}
		var base struct {
			Original v2input.SessionVersionBinding `json:"original_source"`
		}
		if json.Unmarshal([]byte(inventory.BaseReceiptJCS), &base) != nil {
			return empty, false, v2input.ErrInvalid
		}
		queue = append(queue, base.Original.Source)
		for cursor := 0; cursor < len(queue); cursor++ {
			ref := queue[cursor]
			if !validLayerArtifactRef(ref) {
				return empty, false, v2input.ErrInvalid
			}
			if old, ok := ids[ref.ArtifactID]; ok && old != ref {
				return empty, false, v2input.ErrInvalid
			}
			ids[ref.ArtifactID] = ref
			raw, ok := out.Bodies[ref]
			if !ok {
				var e error
				raw, e = objects.ReadJSON(ctx, who, tenant, inventory.Context.Project, parent, ordinal, ref)
				if e != nil {
					return empty, false, e
				}
				if err := store(ref, raw); err != nil {
					return empty, false, err
				}
			}
			if cursor == 0 {
				var m struct {
					Layer      v2input.Ref  `json:"layer_mask"`
					Protection *v2input.Ref `json:"local_protection"`
					Global     *struct {
						Current v2input.Ref `json:"current"`
					} `json:"global_protection"`
				}
				if decodeAuthoredRoot3Mapping(raw, ref, &m) != nil {
					return empty, false, v2input.ErrInvalid
				}
				queue = append(queue, m.Layer)
				if m.Protection != nil {
					queue = append(queue, *m.Protection)
				}
				if m.Global != nil {
					queue = append(queue, m.Global.Current)
				}
			}
		}
	}
	after, found, err := ReadAuthoredGeometryPrefix(ctx, scheduler, tenant, parent, ordinal)
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
