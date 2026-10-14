package delivery

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// SignedAssemblyPublication is the immutable document set that a later
// task-transport transaction must bind to one exact assemble task.
type SignedAssemblyPublication struct {
	Documents SignedAssemblyDocuments
	ShardJCS [][]byte
	AssemblyRef v2input.Ref
	ClosureRef v2input.Ref
	Envelope SignedAssemblyEnvelope
	EnvelopeJCS []byte
	EnvelopeRef v2input.Ref
}

// SignedAssemblyRootPublisher is created only after all shard bodies and refs
// have been compiled. Its implementation must validate those exact bodies at
// both SQL reserve and complete boundaries.
type SignedAssemblyRootPublisher func([][]byte) (PublishDocument, error)

// PublishSignedAssemblyBundle composes the complete required-mode document
// chain. The caller must supply currently verified signer receipts and a
// retained signing closure. Publication grants no task activation authority.
func PublishSignedAssemblyBundle(ctx context.Context, frozen FrozenInput, snapshotRaw []byte,
	slots []Slot, tenant, actor, parent uuid.UUID, dependencies []SigningDependency,
	receipts []SignedDependencyReceipt, shards PublishDocument,
	rootFactory SignedAssemblyRootPublisher, signed PublishDocument,
	) (SignedAssemblyPublication, error) {
	invalid := func() (SignedAssemblyPublication, error) { return SignedAssemblyPublication{}, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil { return SignedAssemblyPublication{}, err }
	if shards == nil || rootFactory == nil || signed == nil { return invalid() }
	var bodies [][]byte
	capture := func(call context.Context, purpose string, raw []byte) (v2input.Ref, error) {
		ref, err := shards(call, purpose, raw)
		if err != nil { return v2input.Ref{}, err }
		if ref.ArtifactID == uuid.Nil || ref.SHA256 != digest(raw) { return v2input.Ref{}, v2input.ErrInvalid }
		bodies = append(bodies, append([]byte(nil), raw...))
		return ref, nil
	}
	documents, err := BuildSignedAssemblyDocuments(ctx, frozen, snapshotRaw, slots,
		tenant, actor, parent, dependencies, receipts, capture)
	if err != nil { return SignedAssemblyPublication{}, err }
	if len(bodies) != len(documents.Assembly.Shards) { return invalid() }
	root, err := rootFactory(bodies)
	if err != nil { return SignedAssemblyPublication{}, err }
	if root == nil { return invalid() }
	if err := ctx.Err(); err != nil { return SignedAssemblyPublication{}, err }
	assemblyRef, err := root(ctx, "v30_delivery_assembly", documents.AssemblyJCS)
	if err != nil { return SignedAssemblyPublication{}, err }
	if assemblyRef.ArtifactID == uuid.Nil || assemblyRef.SHA256 != documents.AssemblySHA { return invalid() }
	if err := ctx.Err(); err != nil { return SignedAssemblyPublication{}, err }
	closureRef, err := signed(ctx, "v30_delivery_signed_closure", documents.ClosureJCS)
	if err != nil { return SignedAssemblyPublication{}, err }
	if closureRef.ArtifactID == uuid.Nil || closureRef.SHA256 != documents.ClosureSHA { return invalid() }
	envelope, envelopeJCS, envelopeSHA, err := CompileSignedAssemblyEnvelope(assemblyRef,
		documents.AssemblyJCS, bodies, closureRef, documents.ClosureJCS,
		tenant, actor, parent, slots, dependencies, receipts)
	if err != nil { return SignedAssemblyPublication{}, err }
	if err := ctx.Err(); err != nil { return SignedAssemblyPublication{}, err }
	envelopeRef, err := signed(ctx, "v30_delivery_signed_envelope", envelopeJCS)
	if err != nil { return SignedAssemblyPublication{}, err }
	if envelopeRef.ArtifactID == uuid.Nil || envelopeRef.SHA256 != envelopeSHA ||
		envelopeRef.ArtifactID == assemblyRef.ArtifactID || envelopeRef.ArtifactID == closureRef.ArtifactID { return invalid() }
	return SignedAssemblyPublication{documents, bodies, assemblyRef, closureRef, envelope, envelopeJCS, envelopeRef}, nil
}
