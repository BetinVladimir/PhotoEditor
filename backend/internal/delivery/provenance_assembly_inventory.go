package delivery

import (
	"bytes"
	"context"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// MergeSignedAssemblyInventory extends the server-resolved ordinary inventory
// with exact roots derived from the published signed documents. It refuses an
// artifact ID whose digest or contract differs between the two authorities.
func MergeSignedAssemblyInventory(base, signed []v2input.Entry) ([]v2input.Entry, error) {
	if len(base) < 3 || len(signed) < 3 || len(base)+len(signed) > 1000000 {
		return nil, v2input.ErrInvalid
	}
	byID := make(map[uuid.UUID]v2input.Entry, len(base)+len(signed))
	for _, group := range [][]v2input.Entry{base, signed} {
		for _, entry := range group {
			if entry.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(entry.Ref.SHA256) ||
				len(entry.Contract) < 1 || len(entry.Contract) > 120 {
				return nil, v2input.ErrInvalid
			}
			if old, exists := byID[entry.Ref.ArtifactID]; exists && old != entry {
				return nil, v2input.ErrInvalid
			}
			byID[entry.Ref.ArtifactID] = entry
		}
	}
	result := make([]v2input.Entry, 0, len(byID))
	for _, entry := range byID { result = append(result, entry) }
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].Ref.ArtifactID, result[j].Ref.ArtifactID
		return bytes.Compare(a[:], b[:]) < 0
	})
	return result, nil
}

// ResolvePublishedSignedAssemblyInputs preserves all transitive source inputs
// reconstructed by current SQL and binds the signed additions to the exact
// physically verified signer set. Signed transport admission repeats the
// entire expected inventory under its final transaction.
func (s ExpansionLeaseStore) ResolvePublishedSignedAssemblyInputs(
	ctx context.Context, lease ExpansionLease, plan PreparePlan,
	publication SignedAssemblyPublication, evidence VerifiedDelegatedSigningSet,
) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	if len(evidence.Slots) != lease.SlotCount || evidence.Actor == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	base, err := s.ResolvePublishedAssemblyInputs(ctx, lease, plan, publication.AssemblyRef)
	if err != nil { return nil, err }
	signed, err := SignedAssemblyRequiredRoots(publication.AssemblyRef,
		publication.Documents.AssemblyJCS, publication.ShardJCS,
		publication.ClosureRef, publication.Documents.ClosureJCS,
		publication.EnvelopeRef, lease.Tenant, evidence.Actor, lease.Run,
		evidence.Slots, evidence.Dependencies, evidence.Receipts)
	if err != nil { return nil, err }
	return MergeSignedAssemblyInventory(base, signed)
}
