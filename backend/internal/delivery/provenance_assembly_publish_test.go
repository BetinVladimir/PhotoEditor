package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestPublishSignedAssemblyBundleOrdersAndBindsDocuments(t *testing.T) {
	frozen, snapshot, slots := assemblyFixture(t, 1)
	tenant, actor, parent, policy := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	frozen.Policy.Provenance = "required"
	frozen.Policy.SigningPolicyID, frozen.Policy.SigningPolicySHA = policy, sha
	dependencies, err := CompileSigningDependencies(parent, policy, slots)
	if err != nil { t.Fatal(err) }
	workflow := uuid.NewSHA1(tenant, []byte("provenance:"+actor.String()+":"+dependencies[0].IdempotencyKey))
	receipt := SignedDependencyReceipt{Signed:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},
		Manifest:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},ValidationSHA:sha,
		TaskID:uuid.NewSHA1(workflow,[]byte("provenance.sign:0")),ResultID:uuid.New(),Bucket:"artifacts",
		SignedObject:objectstore.Object{Key:"tenants/test/signed",VersionID:"v1",SHA256:sha,Bytes:42,ContentType:"image/jpeg"},
		ManifestObject:objectstore.Object{Key:"tenants/test/manifest",VersionID:"v2",SHA256:sha,Bytes:13,ContentType:"application/c2pa"}}
	var order []string
	publish := func(_ context.Context, purpose string, raw []byte)(v2input.Ref,error){
		order=append(order,purpose)
		return v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)},nil
	}
	rootFactory := func(bodies [][]byte)(PublishDocument,error){
		if len(bodies)!=1 || order[0]!="v30_delivery_slots_000" { t.Fatal("root created before complete shards") }
		return publish,nil
	}
	bundle,err:=PublishSignedAssemblyBundle(context.Background(),frozen,snapshot,slots,
		tenant,actor,parent,dependencies,[]SignedDependencyReceipt{receipt},publish,rootFactory,publish)
	if err!=nil { t.Fatal(err) }
	if len(order)!=4 || order[0]!="v30_delivery_slots_000" || order[1]!="v30_delivery_assembly" ||
		order[2]!="v30_delivery_signed_closure" || order[3]!="v30_delivery_signed_envelope" { t.Fatal("publication order",order) }
	if bundle.Envelope.Assembly!=bundle.AssemblyRef || bundle.Envelope.SigningClosure!=bundle.ClosureRef ||
		bundle.EnvelopeRef.SHA256!=digest(bundle.EnvelopeJCS) { t.Fatal("envelope binding") }
 // A publisher/factory may retain or mutate its argument; compiled documents
 // and shard inventory must remain owned by this invocation.
 mutatingPublisher:=func(_ context.Context,_ string,raw []byte)(v2input.Ref,error){
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)}
  for index:=range raw{raw[index]='x'}
  return ref,nil
 }
 mutatingFactory:=func(bodies [][]byte)(PublishDocument,error){
  for _,body:=range bodies{for index:=range body{body[index]='x'}}
  return mutatingPublisher,nil
 }
 isolated,err:=PublishSignedAssemblyBundle(context.Background(),frozen,snapshot,slots,tenant,actor,parent,dependencies,[]SignedDependencyReceipt{receipt},mutatingPublisher,mutatingFactory,mutatingPublisher)
 if err!=nil||isolated.AssemblyRef.SHA256!=digest(isolated.Documents.AssemblyJCS)||isolated.ClosureRef.SHA256!=digest(isolated.Documents.ClosureJCS)||isolated.EnvelopeRef.SHA256!=digest(isolated.EnvelopeJCS){t.Fatal("callback mutation escaped owned documents",err)}
 cancelled,cancel:=context.WithCancel(context.Background())
 cancelLast:=func(call context.Context,purpose string,raw []byte)(v2input.Ref,error){
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)}
  if purpose=="v30_delivery_signed_envelope"{cancel()}
  return ref,nil
 }
 if _,err:=PublishSignedAssemblyBundle(cancelled,frozen,snapshot,slots,tenant,actor,parent,dependencies,[]SignedDependencyReceipt{receipt},publish,rootFactory,cancelLast);err!=context.Canceled{t.Fatal("late envelope cancellation lost",err)}
 cancel()
 mutableSlots:=append([]Slot(nil),slots...)
 mutableDependencies:=append([]SigningDependency(nil),dependencies...)
 mutableReceipts:=[]SignedDependencyReceipt{receipt}
 publisherMutatesInputs:=func(call context.Context,purpose string,raw []byte)(v2input.Ref,error){
  mutableSlots[0].State="omitted"
  mutableDependencies[0].Export.SHA256=strings.Repeat("b",64)
  mutableReceipts[0].Signed.SHA256=strings.Repeat("c",64)
  return v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)},nil
 }
 preserved,err:=PublishSignedAssemblyBundle(context.Background(),frozen,snapshot,mutableSlots,tenant,actor,parent,mutableDependencies,mutableReceipts,publisherMutatesInputs,func([][]byte)(PublishDocument,error){return publisherMutatesInputs,nil},publisherMutatesInputs)
 if err!=nil||preserved.EnvelopeRef.SHA256!=digest(preserved.EnvelopeJCS){t.Fatal("external mutation changed frozen assembly invocation",err)}
	order=nil
	bad:=receipt;bad.TaskID=uuid.New()
	if _,err:=PublishSignedAssemblyBundle(context.Background(),frozen,snapshot,slots,tenant,actor,parent,
		dependencies,[]SignedDependencyReceipt{bad},publish,rootFactory,publish);err==nil||len(order)!=0 {
		t.Fatal("invalid signer produced objects",err,order)
	}
	var assemblyID uuid.UUID
	rootWithIdentity := func([][]byte) (PublishDocument, error) {
		return func(_ context.Context, _ string, raw []byte) (v2input.Ref, error) {
			assemblyID = uuid.New()
			return v2input.Ref{ArtifactID: assemblyID, SHA256: digest(raw)}, nil
		}, nil
	}
	closureCalls := 0
	duplicateClosure := func(_ context.Context, _ string, raw []byte) (v2input.Ref, error) {
		closureCalls++
		return v2input.Ref{ArtifactID: assemblyID, SHA256: digest(raw)}, nil
	}
	if _, err := PublishSignedAssemblyBundle(context.Background(), frozen, snapshot, slots,
		tenant, actor, parent, dependencies, []SignedDependencyReceipt{receipt}, publish,
		rootWithIdentity, duplicateClosure); err == nil || closureCalls != 1 {
		t.Fatal("duplicate assembly/closure artifact identity accepted", err, closureCalls)
	}
	sharedID := uuid.New()
	sharedPublisher := func(_ context.Context, _ string, raw []byte) (v2input.Ref, error) {
		return v2input.Ref{ArtifactID: sharedID, SHA256: digest(raw)}, nil
	}
	signedCalls := 0
	countSigned := func(ctx context.Context, purpose string, raw []byte) (v2input.Ref, error) {
		signedCalls++
		return publish(ctx, purpose, raw)
	}
	if _, err := PublishSignedAssemblyBundle(context.Background(), frozen, snapshot, slots,
		tenant, actor, parent, dependencies, []SignedDependencyReceipt{receipt}, sharedPublisher,
		func([][]byte) (PublishDocument, error) { return sharedPublisher, nil }, countSigned); err == nil || signedCalls != 0 {
		t.Fatal("duplicate shard/assembly artifact identity accepted", err, signedCalls)
	}
}
