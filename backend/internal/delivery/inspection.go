package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

type Inspection struct {
	PackageID    uuid.UUID   `json:"package_id"`
	Revision     string      `json:"revision"`
	Manifest     v2input.Ref `json:"manifest"`
	SlotCount    int         `json:"slot_count"`
	ReadyCount   int         `json:"ready_count"`
	OmittedCount int         `json:"omitted_count"`
}

// ValidInspectionResource checks only public response binding/shape. Physical
// readiness is proven by ReadInspection's SQL and canonical manifest checks.
func ValidInspectionResource(value Inspection, id uuid.UUID, revision string) bool {
	q := ItemsQuery{Revision: revision, AfterOrdinal: -1, Limit: 1}
	return id != uuid.Nil && q.Validate() == nil && value.PackageID == id && value.Revision == revision &&
		value.Manifest.ArtifactID != uuid.Nil && shaPattern.MatchString(value.Manifest.SHA256) &&
		value.SlotCount >= 1 && value.SlotCount <= 80000 && value.ReadyCount >= 1 && value.ReadyCount <= value.SlotCount &&
		value.OmittedCount >= 0 && value.OmittedCount <= value.SlotCount && value.ReadyCount+value.OmittedCount == value.SlotCount
}

func validInspectionWireShape(raw []byte) bool {
	root, ok := closedJSONObject(raw)
	if !ok || len(root) != 4 {
		return false
	}
	for _, key := range []string{"package_id", "revision", "manifest", "expected_manifest"} {
		if _, exists := root[key]; !exists {
			return false
		}
	}
	manifest, ok := closedJSONObject(root["manifest"])
	if !ok || len(manifest) != 2 {
		return false
	}
	for _, key := range []string{"artifact_id", "sha256"} {
		if _, exists := manifest[key]; !exists {
			return false
		}
	}
	expected, ok := closedJSONObject(root["expected_manifest"])
	if !ok || len(expected) != 7 {
		return false
	}
	for _, key := range []string{"schema_version", "assembly_sha", "snapshot_digest", "slot_count", "ready_count", "omitted_count", "shards"} {
		if _, exists := expected[key]; !exists {
			return false
		}
	}
	var shards []json.RawMessage
	if json.Unmarshal(expected["shards"], &shards) != nil || len(shards) < 1 || len(shards) > 160 {
		return false
	}
	for _, rawShard := range shards {
		shard, ok := closedJSONObject(rawShard)
		if !ok || len(shard) != 2 {
			return false
		}
		for _, key := range []string{"artifact_id", "sha256"} {
			if _, exists := shard[key]; !exists {
				return false
			}
		}
	}
	return true
}

// Reconstruct exact canonical manifest bytes from SQL-authorized frozen slots.
// SQL proves current scope/producer/inventory; matching SHA binds the public proof.
func ValidateInspection(raw []byte, id uuid.UUID, revision string) (Inspection, error) {
	invalid := func() (Inspection, error) { return Inspection{}, domain.NewError("DELIVERY_INSPECTION_INVALID", 500) }
	if len(raw) < 2 || len(raw) > 1<<20 || id == uuid.Nil || !validInspectionWireShape(raw) {
		return invalid()
	}
	if (ItemsQuery{Revision: revision, AfterOrdinal: -1, Limit: 1}).Validate() != nil {
		return invalid()
	}
	var wire struct {
		PackageID uuid.UUID   `json:"package_id"`
		Revision  string      `json:"revision"`
		Manifest  v2input.Ref `json:"manifest"`
		Expected  Manifest    `json:"expected_manifest"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return invalid()
	}
	m := wire.Expected
	if wire.PackageID != id || wire.Revision != revision || wire.Manifest.ArtifactID == uuid.Nil || !shaPattern.MatchString(wire.Manifest.SHA256) || m.SchemaVersion != 1 || !shaPattern.MatchString(m.AssemblySHA) || !shaPattern.MatchString(m.SnapshotDigest) || m.SlotCount < 1 || m.SlotCount > 80000 || m.ReadyCount < 1 || m.ReadyCount > m.SlotCount || m.OmittedCount < 0 || m.OmittedCount > m.SlotCount || m.ReadyCount+m.OmittedCount != m.SlotCount || len(m.Shards) < 1 || len(m.Shards) > 160 || len(m.Shards) != (m.SlotCount+499)/500 {
		return invalid()
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range m.Shards {
		if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || seen[ref.ArtifactID] {
			return invalid()
		}
		seen[ref.ArtifactID] = true
	}
	body, err := canonical.Marshal(m)
	if err != nil || digest(body) != wire.Manifest.SHA256 {
		return invalid()
	}
	return Inspection{PackageID: id, Revision: revision, Manifest: wire.Manifest, SlotCount: m.SlotCount, ReadyCount: m.ReadyCount, OmittedCount: m.OmittedCount}, nil
}
func ReadInspection(ctx context.Context, tx ProofTransaction, who auth.Identity, tid, id uuid.UUID, revision string) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	q := ItemsQuery{Revision: revision, AfterOrdinal: -1, Limit: 1}
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || id == uuid.Nil {
		return Inspection{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := q.Validate(); err != nil {
		return Inspection{}, err
	}
	n, _ := strconv.ParseInt(revision, 10, 64)
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT photo.inspect_v30_delivery($1,$2,$3,$4,$5)`, tid, id, who.UserID, who.SessionID, n).Scan(&raw); err != nil {
		return Inspection{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return Inspection{}, domain.NewError("DELIVERY_INSPECTION_STALE", 409)
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	return ValidateInspection(raw, id, revision)
}
func (s Service) Inspect(ctx context.Context, who auth.Identity, tid, id uuid.UUID, revision string) (Inspection, error) {
	if s.DB == nil {
		return Inspection{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var result Inspection
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var err error
		result, err = ReadInspection(ctx, tx, who, tid, id, revision)
		return err
	})
	return result, err
}
