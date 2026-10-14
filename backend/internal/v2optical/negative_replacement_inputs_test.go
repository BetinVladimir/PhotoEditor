package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementInputPackageRequiresWholeBranchAndOwnsEvidence(t *testing.T){
 _,source,_,_:=negativeDraftFixture(t);spec,object,_:=perspectiveArrayFixture(t,3,[]float32{-.5,1.5,.25})
 image,err:=review.ParseWorkingImage(source);if err!=nil {t.Fatal(err)};image.Canvas.Width=1;image.Canvas.Height=1;image.Alpha=nil;image.Pixels.ID=spec.Ref.ArtifactID;image.Pixels.SHA256=spec.Ref.SHA256
 source,err=canonical.Marshal(image);if err!=nil {t.Fatal(err)};sum:=sha256.Sum256(source)
 tenant,project,reviewID,origin,run,child,childRun:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};hash:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 selection:=VerifiedNegativeSelection{source:negativeSelectedSource{SchemaVersion:1,Tenant:tenant,Project:project,Review:reviewID,Task:origin,Run:run,Hash:hash,Route:"base",Source:&negativeSourceLocator{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:]),Bucket:"photos",Key:"source",Version:"original",Bytes:int64(len(source)),Media:"application/json"}},raw:string(source)}
 physical:=VerifiedNegativeSelectedArrays{selection:selection,specs:[]NegativeSourceArraySpec{spec},objects:[]objectstore.Object{object},who:who}
 p:=perspectiveIntent();params,_,err:=p.Compile();if err!=nil {t.Fatal(err)};paramsSHA:=sha256.Sum256(params)
 frozen:=VerifiedOpticalFrozenIntent{locator:negativeFrozenIntentLocator{SchemaVersion:1,Tenant:tenant,Project:project,Review:reviewID,Hash:hash,Task:child,Run:childRun,Operation:"optical.perspective",Params:negativeSourceLocator{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(paramsSHA[:]),Bucket:"photos",Key:"params",Version:"original",Bytes:int64(len(params)),Media:"application/json"}},raw:string(params),who:who}
 inventory:=[]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}},{TaskID:child,RunID:childRun,Parents:[]uuid.UUID{origin}}}
 plan,err:=PlanNegativeInvalidation(context.Background(),origin,run,"base",inventory,10,10);if err!=nil {t.Fatal(err)}
 prepare:=func(intents []VerifiedOpticalFrozenIntent)(PreparedNegativeReplacementInputs,error){return PrepareNegativeReplacementInputs(context.Background(),"photos",who,tenant,project,reviewID,hash,plan,inventory,physical,intents)}
 prepared,err:=prepare([]VerifiedOpticalFrozenIntent{frozen});if err!=nil {t.Fatal("complete packet",err)}
 for _,bad:=range [][]VerifiedOpticalFrozenIntent{nil,{frozen,frozen}} {if _,err:=prepare(bad);err==nil {t.Fatal("partial/duplicate branch accepted")}}
 foreign:=frozen;foreign.locator.Run=uuid.New();if _,err:=prepare([]VerifiedOpticalFrozenIntent{foreign});err==nil {t.Fatal("foreign original run accepted")}
 view,err:=prepared.Topology();if err!=nil||len(view.Nodes)!=1||view.Nodes[0].OriginalTask!=child {t.Fatal("topology",err)}
 bound,err:=prepared.ReadyParentSources(context.Background(),nil,nil);if err!=nil||len(bound)!=1||bound[0].Step().Node().OriginalTask!=child {t.Fatal("selected-base parent wave",err)}
 after:=p.Base;after.Source=*physical.selection.SelectedBase();after.CanvasHash,err=canonical.Hash(image.Canvas);if err!=nil {t.Fatal(err)}
 lineRef:=&v2input.Ref{ArtifactID:uuid.New(),SHA256:hash};if p.Mode!="auto" {lineRef=nil}
 if _,err:=bound[0].Rebind(context.Background(),[]OpticalReplacementBinding{{Before:p.Base,After:after}},lineRef);err!=nil {t.Fatal("bound selected source rebind",err)}
 duplicateBefore:=p.Base;duplicateBefore.AssetVersionID=uuid.New()
 if _,err:=bound[0].Rebind(context.Background(),[]OpticalReplacementBinding{{Before:p.Base,After:after},{Before:duplicateBefore,After:after}},lineRef);err==nil {t.Fatal("same rebuilt source consumed twice")}
 wrongAfter:=after;wrongAfter.Source.ArtifactID=uuid.New()
 if _,err:=bound[0].Rebind(context.Background(),[]OpticalReplacementBinding{{Before:p.Base,After:wrongAfter}},lineRef);err==nil {t.Fatal("arbitrary replacement source accepted")}
 wrongAfter=after;wrongAfter.CanvasHash=hash
 if wrongAfter.CanvasHash==after.CanvasHash {wrongAfter.CanvasHash="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
 if _,err:=bound[0].Rebind(context.Background(),[]OpticalReplacementBinding{{Before:p.Base,After:wrongAfter}},lineRef);err==nil {t.Fatal("substituted canvas accepted")}
 if _,err:=prepared.ReadyParentSources(context.Background(),[]VerifiedAdoptedParentArrays{{}},nil);err==nil {t.Fatal("empty completion proof accepted")}
 if _,err:=prepared.ReadyParentSources(context.Background(),nil,[]VerifiedAdoptedParentArrays{{}});err==nil {t.Fatal("empty retained proof accepted")}
 steps,err:=prepared.ReadySteps(context.Background(),nil);if err!=nil||len(steps)!=1||steps[0].Operation!="optical.perspective" {t.Fatal("ready frozen intent",err)}
 mappingRaw,_:=canonical.Marshal([]negativeAffectedSource{{Artifact:p.Base.Source.ArtifactID,SHA:p.Base.Source.SHA256,Producer:origin,Run:run}})
 mappingCatalog:=&negativeAffectedMappingDouble{raw:mappingRaw}
 mapping,err:=loadNegativeAffectedSources(context.Background(),mappingCatalog,steps[0]);if err!=nil||len(mapping.sources)!=1||mapping.sources[0].Before!=p.Base||mappingCatalog.calls!=2 {t.Fatal("scoped mapping receipt",err)}
 mappedLines:=&v2input.Ref{ArtifactID:uuid.New(),SHA256:hash};if p.Mode!="auto" {mappedLines=nil}
 mapped,err:=bound[0].RebindMapped(context.Background(),mapping,mappedLines);if err!=nil {t.Fatal("automatic source replacement",err)}
 mappedStep:=PreparedMappedReplacementStep{parents:bound[0],mapping:mapping}
 publication,publicationSHA,err:=mappedStep.PublicationIdentity(context.Background(),mappedLines);if err!=nil||publication.Task!=mappedStep.Node().Task||publication.Run!=mappedStep.Node().Run||publication.ParamsSHA!=mapped.SHA256()||len(publicationSHA)!=64 {t.Fatal("replacement publication identity",err)}
 againPublication,againPublicationSHA,err:=mappedStep.PublicationIdentity(context.Background(),mappedLines);if err!=nil||againPublication!=publication||againPublicationSHA!=publicationSHA {t.Fatal("unstable publication retry identity",err)}
 publication.ParamsSHA="changed";if againPublication.ParamsSHA!=mapped.SHA256() {t.Fatal("mutable publication identity")}
 markerRaw,markerSHA,err:=mappedStep.RunMarker(context.Background());if err!=nil||len(markerRaw)==0||len(markerSHA)!=64 {t.Fatal("deterministic run marker",err)}
 var marker negativeReplacementRunMarker;if err:=json.Unmarshal(markerRaw,&marker);err!=nil||marker.OriginalTask!=child||marker.OriginalRun!=childRun||marker.Version!="v29_negative_replacement_v2" {t.Fatal("marker scope",err)}
 markerRaw[0]='[';againMarker,_,err:=mappedStep.RunMarker(context.Background());if err!=nil||againMarker[0]=='[' {t.Fatal("marker ownership",err)}
 badMarkerStep:=mappedStep;badMarkerStep.parents.step.node.Run=uuid.New()
 if _,_,err:=badMarkerStep.RunMarker(context.Background());err==nil {t.Fatal("foreign replacement run marker accepted")}
 document,err:=mappedStep.sourceDocument(context.Background(),*physical.selection.SelectedBase());if err!=nil||string(document)!=string(source) {t.Fatal("owned source document",err)}
 document[0]='[';if physical.selection.WorkingImageBytes()[0]=='[' {t.Fatal("mutable compiled source")}
 if _,err:=mappedStep.sourceDocument(context.Background(),v2input.Ref{ArtifactID:uuid.New(),SHA256:hash});err==nil {t.Fatal("unproved source document accepted")}
 mappedParams,err:=ParsePerspectiveParams(mapped.Params());if err!=nil||mappedParams.Base.Source!=*physical.selection.SelectedBase()||mappedParams.Base.AssetVersionID!=p.Base.AssetVersionID {t.Fatal("mapped source/identity",err)}
 foreignMapping:=mapping;foreignMapping.step.node.Task=uuid.New()
 if _,err:=bound[0].RebindMapped(context.Background(),foreignMapping,mappedLines);err==nil {t.Fatal("foreign step mapping accepted")}
 corruptMapping:=mapping;corruptMapping.raw="[]"
 if _,err:=bound[0].RebindMapped(context.Background(),corruptMapping,mappedLines);err==nil {t.Fatal("changed mapping packet accepted")}
 if err:=prepared.LockAffectedSources(context.Background(),nil,mapping);err!=v2input.ErrInvalid {t.Fatal("mapping nil transaction accepted",err)}
 invalidMappingTx:=&negativeReplacementScopeTx{}
 if err:=prepared.LockAffectedSources(context.Background(),invalidMappingTx,corruptMapping);err==nil||invalidMappingTx.gates!=0 {t.Fatal("corrupt mapping reached SQL",err)}
 if err:=prepared.LockUnchangedSource(context.Background(),nil,VerifiedNegativeUnchangedArrays{});err!=v2input.ErrInvalid {t.Fatal("nil unchanged transaction accepted",err)}
 emptyUnchangedTx:=&negativeReplacementScopeTx{}
 if err:=prepared.LockUnchangedSource(context.Background(),emptyUnchangedTx,VerifiedNegativeUnchangedArrays{});err==nil||emptyUnchangedTx.gates!=0 {t.Fatal("empty unchanged proof reached SQL",err)}
 missingMapTx:=&negativeReplacementScopeTx{}
 if _,err:=prepared.LockMappedReadySources(context.Background(),missingMapTx,nil,nil,nil);err==nil||missingMapTx.gates!=0 {t.Fatal("incomplete wave reached SQL",err)}
 corruptMapTx:=&negativeReplacementScopeTx{}
 if _,err:=prepared.LockMappedReadySources(context.Background(),corruptMapTx,nil,nil,[]VerifiedNegativeAffectedSources{corruptMapping});err==nil||corruptMapTx.gates!=0 {t.Fatal("corrupt wave map reached SQL",err)}
 mappedScopeTx:=&negativeReplacementScopeTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},refuse:true}
 if ready,err:=prepared.LockMappedReadySources(context.Background(),mappedScopeTx,nil,nil,[]VerifiedNegativeAffectedSources{mapping});err!=v2input.ErrExpired||len(ready)!=0||mappedScopeTx.gates!=1 {t.Fatal("mapped wave ignored scope refusal",err)}
 preparedWave,err:=prepared.PrepareCompleteMappedSources(context.Background(),nil,nil,[]VerifiedNegativeAffectedSources{mapping},nil);if err!=nil||len(preparedWave)!=1||preparedWave[0].Node().OriginalTask!=child {t.Fatal("pure complete source wave",err)}
 preparedRebound,err:=preparedWave[0].Rebind(context.Background(),mappedLines);if err!=nil||preparedRebound.SHA256()!=mapped.SHA256() {t.Fatal("pure preparation changed replacement",err)}
 if _,err:=prepared.PrepareCompleteMappedSources(context.Background(),nil,nil,nil,nil);err==nil {t.Fatal("pure wave ignored missing mapping")}
 completeScopeTx:=&negativeReplacementScopeTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},refuse:true}
 if ready,err:=prepared.LockCompleteMappedSources(context.Background(),completeScopeTx,nil,nil,[]VerifiedNegativeAffectedSources{mapping},nil);err!=v2input.ErrExpired||len(ready)!=0||completeScopeTx.gates!=1 {t.Fatal("complete wave ignored scope refusal",err)}
 foreignUnchangedTx:=&negativeReplacementScopeTx{}
 if _,err:=prepared.LockCompleteMappedSources(context.Background(),foreignUnchangedTx,nil,nil,[]VerifiedNegativeAffectedSources{mapping},[]VerifiedNegativeUnchangedArrays{{}});err==nil||foreignUnchangedTx.gates!=0 {t.Fatal("foreign unchanged proof reached SQL",err)}
 mappingCatalog=&negativeAffectedMappingDouble{raw:mappingRaw,changed:true}
 if _,err:=loadNegativeAffectedSources(context.Background(),mappingCatalog,steps[0]);err!=v2input.ErrExpired {t.Fatal("changed source mapping accepted",err)}
 stepNode:=steps[0].Node();if stepNode.OriginalTask!=child||stepNode.OriginalRun!=childRun {t.Fatal("wrong step identity")}
 stepNode.Parents[0].Task=uuid.New();if steps[0].Node().Parents[0].Task!=uuid.Nil {t.Fatal("step parent ownership")}
 finished,err:=prepared.ReadySteps(context.Background(),[]uuid.UUID{steps[0].Node().Task});if err!=nil||len(finished)!=0 {t.Fatal("completed step repeated",err)}
 if _,err:=prepared.ReadySteps(context.Background(),[]uuid.UUID{child});err==nil {t.Fatal("original identity accepted as completion")}
 broken:=prepared;broken.fingerprint="broken";if _,err:=broken.ReadySteps(context.Background(),nil);err==nil {t.Fatal("corrupt packet reached compiler")}
 if _,err:=(PreparedNegativeReplacementStep{}).Rebind(context.Background(),nil,nil);err==nil {t.Fatal("empty step rebound")}
 view.Nodes[0].Task=uuid.Nil;again,_:=prepared.Topology();if again.Nodes[0].Task==uuid.Nil {t.Fatal("mutable topology")}
 physical.objects[0].VersionID="changed";physical.selection.source.Source.Version="changed";inventory[1].Parents[0]=uuid.New()
 if prepared.physical.objects[0].VersionID!=object.VersionID||prepared.physical.selection.source.Source.Version!="original" {t.Fatal("caller mutation leaked")}
 tx:=&negativeReplacementScopeTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},refuse:true}
 if err:=prepared.Lock(context.Background(),tx);err!=v2input.ErrExpired||tx.gates!=1 {t.Fatal("scope refusal did not stop packet",err)}
 if _,err:=prepared.LockReadyParentSources(context.Background(),nil,nil,nil);err!=v2input.ErrInvalid {t.Fatal("nil wave transaction accepted",err)}
 waveTx:=&negativeReplacementScopeTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:tenant}},refuse:true}
 if ready,err:=prepared.LockReadyParentSources(context.Background(),waveTx,nil,nil);err!=v2input.ErrExpired||len(ready)!=0||waveTx.gates!=1 {t.Fatal("wave scope refusal did not stop",err)}
 emptyProofTx:=&negativeReplacementScopeTx{}
 if _,err:=prepared.LockReadyParentSources(context.Background(),emptyProofTx,[]VerifiedAdoptedParentArrays{{}},nil);err==nil||emptyProofTx.gates!=0 {t.Fatal("invalid wave proof reached SQL",err)}
 cancelled,cancelWave:=context.WithCancel(context.Background());cancelWave()
 if _,err:=prepared.LockReadyParentSources(cancelled,waveTx,nil,nil);err!=context.Canceled {t.Fatal("wave cancellation",err)}

 corrupted:=prepared;corrupted.fingerprint="changed";tx=&negativeReplacementScopeTx{}
 if err:=corrupted.Lock(context.Background(),tx);err!=v2input.ErrInvalid||tx.gates!=0 {t.Fatal("corrupt packet reached SQL",err)}
}

type negativeAffectedMappingDouble struct{raw []byte;calls int;changed bool}
func(c *negativeAffectedMappingDouble)lookup(context.Context,PreparedNegativeReplacementStep)([]byte,error){c.calls++;if c.changed&&c.calls>1{return []byte("[]"),nil};return append([]byte(nil),c.raw...),nil}
