package delivery

import (
	"context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"

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
 // Own the complete invocation before the first external publication callback.
 // The same frozen slots/receipts must drive both documents and final envelope.
 frozenRaw,copyErr:=canonical.Marshal(frozen)
 if copyErr!=nil||json.Unmarshal(frozenRaw,&frozen)!=nil{return invalid()}
 snapshotRaw=append([]byte(nil),snapshotRaw...)
 ownedSlots:=make([]Slot,len(slots))
 for index,slot:=range slots{
  ownedSlots[index]=slot
  if slot.ExportID!=nil{value:=*slot.ExportID;ownedSlots[index].ExportID=&value}
  if slot.Export!=nil{value:=*slot.Export;ownedSlots[index].Export=&value}
  if slot.Reason!=nil{value:=*slot.Reason;ownedSlots[index].Reason=&value}
 }
 slots=ownedSlots
 dependencies=append([]SigningDependency(nil),dependencies...)
 receipts=append([]SignedDependencyReceipt(nil),receipts...)
	var bodies [][]byte
	artifactIDs := make(map[uuid.UUID]struct{})
	unique := func(ref v2input.Ref) bool {
		if ref.ArtifactID == uuid.Nil { return false }
		if _, exists := artifactIDs[ref.ArtifactID]; exists { return false }
		artifactIDs[ref.ArtifactID] = struct{}{}
		return true
	}
	capture := func(call context.Context, purpose string, raw []byte) (v2input.Ref, error) {
		ref, err := shards(call, purpose, append([]byte(nil), raw...))
		if err != nil { return v2input.Ref{}, err }
		if !unique(ref) || ref.SHA256 != digest(raw) { return v2input.Ref{}, v2input.ErrInvalid }
		bodies = append(bodies, append([]byte(nil), raw...))
		return ref, nil
	}
	documents, err := BuildSignedAssemblyDocuments(ctx, frozen, snapshotRaw, slots,
		tenant, actor, parent, dependencies, receipts, capture)
	if err != nil { return SignedAssemblyPublication{}, err }
	if len(bodies) != len(documents.Assembly.Shards) { return invalid() }
	ownedBodies:=make([][]byte,len(bodies))
 for index,body:=range bodies{ownedBodies[index]=append([]byte(nil),body...)}
 root, err := rootFactory(ownedBodies)
	if err != nil { return SignedAssemblyPublication{}, err }
	if root == nil { return invalid() }
	if err := ctx.Err(); err != nil { return SignedAssemblyPublication{}, err }
	assemblyRef, err := root(ctx, "v30_delivery_assembly", append([]byte(nil),documents.AssemblyJCS...))
	if err != nil { return SignedAssemblyPublication{}, err }
	if !unique(assemblyRef) || assemblyRef.SHA256 != documents.AssemblySHA { return invalid() }
	if err := ctx.Err(); err != nil { return SignedAssemblyPublication{}, err }
	closureRef, err := signed(ctx, "v30_delivery_signed_closure", append([]byte(nil),documents.ClosureJCS...))
	if err != nil { return SignedAssemblyPublication{}, err }
	if !unique(closureRef) || closureRef.SHA256 != documents.ClosureSHA { return invalid() }
	envelope, envelopeJCS, envelopeSHA, err := CompileSignedAssemblyEnvelope(assemblyRef,
		documents.AssemblyJCS, bodies, closureRef, documents.ClosureJCS,
		tenant, actor, parent, slots, dependencies, receipts)
	if err != nil { return SignedAssemblyPublication{}, err }
	if err := ctx.Err(); err != nil { return SignedAssemblyPublication{}, err }
	envelopeRef, err := signed(ctx, "v30_delivery_signed_envelope", append([]byte(nil),envelopeJCS...))
	if err != nil { return SignedAssemblyPublication{}, err }
 if err:=ctx.Err();err!=nil{return SignedAssemblyPublication{},err}
	if !unique(envelopeRef) || envelopeRef.SHA256 != envelopeSHA { return invalid() }
	return SignedAssemblyPublication{documents, bodies, assemblyRef, closureRef, envelope, envelopeJCS, envelopeRef}, nil
}
