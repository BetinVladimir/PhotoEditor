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
	order=nil
	bad:=receipt;bad.TaskID=uuid.New()
	if _,err:=PublishSignedAssemblyBundle(context.Background(),frozen,snapshot,slots,tenant,actor,parent,
		dependencies,[]SignedDependencyReceipt{bad},publish,rootFactory,publish);err==nil||len(order)!=0 {
		t.Fatal("invalid signer produced objects",err,order)
	}
}
