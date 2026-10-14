package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// ReadPublishedAssembly recovers exact root bytes under current parent authority.
// SQL rechecks every durable shard against the current terminal export matrix.
// The result is evidence for admission; it does not authorize later mutation.
func (s ExpansionLeaseStore) ReadPublishedAssembly(ctx context.Context, lease ExpansionLease, plan PreparePlan, ref v2input.Ref) (AssemblyInput, []byte, error) {
	invalid := func() (AssemblyInput, []byte, error) { return AssemblyInput{}, nil, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil {
		return AssemblyInput{}, nil, err
	}
	if s.DB == nil || !lease.valid() || lease.ParentTask != plan.task || lease.Cursor != lease.SlotCount || lease.SlotCount != plan.preparation.SlotCount || ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) {
		return invalid()
	}
	var raw *string
	if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_saved_assembly($1,$2,$3,$4)`, lease.Expansion, lease.Token, ref.ArtifactID, ref.SHA256).Scan(&raw); err != nil {
		return AssemblyInput{}, nil, err
	}
	if raw == nil {
		return AssemblyInput{}, nil, domain.NewError("DELIVERY_ASSEMBLY_STALE", 409)
	}
	if len(*raw) < 2 || len(*raw) > 1<<20 || digest([]byte(*raw)) != ref.SHA256 {
		return invalid()
	}
	var input AssemblyInput
	decoder := json.NewDecoder(bytes.NewReader([]byte(*raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return invalid()
	}
	if len(input.Shards) != (lease.SlotCount+499)/500 {
		return invalid()
	}
	seen := map[uuid.UUID]bool{}
	for _, shard := range input.Shards {
		if shard.ArtifactID == uuid.Nil || seen[shard.ArtifactID] || !shaPattern.MatchString(shard.SHA256) || shard.ArtifactID == ref.ArtifactID {
			return invalid()
		}
		seen[shard.ArtifactID] = true
	}
	var proof PrepareProof
	var frozen FrozenInput
	if json.Unmarshal(plan.proofJCS, &proof) != nil || json.Unmarshal([]byte(proof.FrozenInputJCS), &frozen) != nil {
		return invalid()
	}
	sort.Slice(frozen.Presets, func(i, j int) bool { return frozen.Presets[i].PresetID.String() < frozen.Presets[j].PresetID.String() })
	expected := AssemblyInput{SchemaVersion: 1, SnapshotDigest: frozen.SnapshotDigest, ItemCount: len(plan.items), SlotCount: plan.preparation.SlotCount, Policy: frozen.Policy, Presets: frozen.Presets, Shards: input.Shards}
	exact, err := canonical.Marshal(expected)
	if err != nil || !bytes.Equal(exact, []byte(*raw)) {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return AssemblyInput{}, nil, err
	}
	return input, append([]byte(nil), exact...), nil
}

// RetainPublishedAssembly attaches the server-reconstructed closure atomically.
// Call outside object IO; SQL derives tenant/workflow/refs from current evidence.
func (s ExpansionLeaseStore) RetainPublishedAssembly(ctx context.Context, lease ExpansionLease, plan PreparePlan, ref v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || !lease.valid() || lease.ParentTask != plan.task || lease.Cursor != lease.SlotCount || lease.SlotCount != plan.preparation.SlotCount || ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) {
		return v2input.ErrInvalid
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.retain_v30_delivery_assembly($1,$2,$3,$4)`, lease.Expansion, lease.Token, ref.ArtifactID, ref.SHA256).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_ASSEMBLY_STALE", 409)
	}
	return nil
}

// ReadPublishedAssemblyInputs recovers each bounded shard with fresh SQL slot
// authority and rechecks the entire root after the pass. Results are evidence;
// registration/activation must repeat source authority in their transaction.
func (s ExpansionLeaseStore) ReadPublishedAssemblyInputs(ctx context.Context, lease ExpansionLease, plan PreparePlan, ref v2input.Ref) ([]v2input.Entry, error) {
	root, shards, err := s.ReadPublishedAssemblyDocuments(ctx, lease, plan, ref)
	if err != nil {
		return nil, err
	}
	return plan.AssemblyRequiredInputs(ctx, lease.Run, ref, root, shards)
}

// ReadPublishedAssemblyDocuments returns owned exact current root/shard bytes.
func (s ExpansionLeaseStore) ReadPublishedAssemblyDocuments(ctx context.Context, lease ExpansionLease, plan PreparePlan, ref v2input.Ref) ([]byte, [][]byte, error) {
	input, root, err := s.ReadPublishedAssembly(ctx, lease, plan, ref)
	if err != nil {
		return nil, nil, err
	}
	shards := make([][]byte, 0, len(input.Shards))
	for index, shardRef := range input.Shards {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if err := s.Renew(ctx, lease); err != nil {
			return nil, nil, err
		}
		var raw *string
		if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_saved_shard($1,$2,$3,$4,$5)`, lease.Expansion, lease.Token, ref.ArtifactID, ref.SHA256, index).Scan(&raw); err != nil {
			return nil, nil, err
		}
		if raw == nil {
			return nil, nil, domain.NewError("DELIVERY_ASSEMBLY_STALE", 409)
		}
		if len(*raw) < 2 || len(*raw) > 1<<20 || digest([]byte(*raw)) != shardRef.SHA256 {
			return nil, nil, v2input.ErrInvalid
		}
		shards = append(shards, []byte(*raw))
	}
	if err := s.Renew(ctx, lease); err != nil {
		return nil, nil, err
	}
	_, again, err := s.ReadPublishedAssembly(ctx, lease, plan, ref)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(root, again) {
		return nil, nil, domain.NewError("DELIVERY_MATRIX_CHANGED", 409)
	}
	if _, err := plan.AssemblyRequiredInputs(ctx, lease.Run, ref, root, shards); err != nil {
		return nil, nil, err
	}
	return root, shards, nil
}

// ResolvePublishedAssemblyInputs recovers server-derived transitive input entries
// after independently deriving mandatory roots from exact documents. No object
// locations or caller-inferred source refs are accepted. Current SQL readiness
// and parent retention are rechecked by the inventory reader itself.
func (s ExpansionLeaseStore) ResolvePublishedAssemblyInputs(ctx context.Context, lease ExpansionLease, plan PreparePlan, ref v2input.Ref) ([]v2input.Entry, error) {
	required, err := s.ReadPublishedAssemblyInputs(ctx, lease, plan, ref)
	if err != nil {
		return nil, err
	}
	return s.resolveAssemblyInventory(ctx, lease, ref, required)
}

func (s ExpansionLeaseStore) resolveAssemblyInventory(ctx context.Context, lease ExpansionLease, ref v2input.Ref, required []v2input.Entry) ([]v2input.Entry, error) {
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_inventory($1,$2,$3,$4)`, lease.Expansion, lease.Token, ref.ArtifactID, ref.SHA256).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, domain.NewError("DELIVERY_ASSEMBLY_STALE", 409)
	}
	if len(raw) > 256<<20 {
		return nil, v2input.ErrInvalid
	}
	var entries []v2input.Entry
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&entries) != nil || decoder.Decode(new(any)) != io.EOF || len(entries) < 3 || len(entries) > 999999 {
		return nil, v2input.ErrInvalid
	}
	inventory := make(map[uuid.UUID]v2input.Entry, len(entries))
	var previous uuid.UUID
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(entry.Ref.SHA256) || len(entry.Contract) < 1 || len(entry.Contract) > 120 || entry.Contract == "Params" || entry.Contract == "InputClosureChunk" || entry.Contract == "InputClosureManifest" || index > 0 && bytes.Compare(previous[:], entry.Ref.ArtifactID[:]) >= 0 {
			return nil, v2input.ErrInvalid
		}
		inventory[entry.Ref.ArtifactID] = entry
		previous = entry.Ref.ArtifactID
	}
	for _, entry := range required {
		if inventory[entry.Ref.ArtifactID] != entry {
			return nil, v2input.ErrInvalid
		}
	}
	if err := s.Renew(ctx, lease); err != nil {
		return nil, err
	}
	return entries, nil
}

// AssemblyPreparationEvidence carries owned bytes and server-resolved inventory.
// It is not authorization for a later transaction; admission repeats authority.
type AssemblyPreparationEvidence struct {
	RootJCS   []byte
	ShardsJCS [][]byte
	Inventory []v2input.Entry
}

func (s ExpansionLeaseStore) ReadAssemblyPreparation(ctx context.Context, lease ExpansionLease, plan PreparePlan, ref v2input.Ref) (AssemblyPreparationEvidence, error) {
	root, shards, err := s.ReadPublishedAssemblyDocuments(ctx, lease, plan, ref)
	if err != nil {
		return AssemblyPreparationEvidence{}, err
	}
	required, err := plan.AssemblyRequiredInputs(ctx, lease.Run, ref, root, shards)
	if err != nil {
		return AssemblyPreparationEvidence{}, err
	}
	inventory, err := s.resolveAssemblyInventory(ctx, lease, ref, required)
	if err != nil {
		return AssemblyPreparationEvidence{}, err
	}
	return AssemblyPreparationEvidence{RootJCS: root, ShardsJCS: shards, Inventory: inventory}, nil
}
