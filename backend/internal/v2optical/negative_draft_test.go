package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "os"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2qualification"
)

func negativeDraftFixture(t *testing.T)(NegativeParams,[]byte,VerifiedNegativeQualification,NegativeResourceEstimate){
 t.Helper()
 raw,err:=os.ReadFile("testdata/negative-qualification.json");if err!=nil{t.Fatal(err)}
 var f struct{Params NegativeParams;Calibration,Report string;Pin NegativeQualificationPin;Locks v2qualification.DeploymentLocks}
 if json.Unmarshal(raw,&f)!=nil{t.Fatal("invalid qualification fixture")}
 source,err:=os.ReadFile("testdata/negative-source.json");if err!=nil{t.Fatal(err)}
 image,err:=review.ParseWorkingImage(source);if err!=nil{t.Fatal(err)}
 f.Params.Base.Source.SHA256,err=canonical.Hash(image);if err!=nil{t.Fatal(err)}
 f.Params.Base.CanvasHash,err=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 qualified,err:=VerifyNegativeQualificationEvidence(f.Pin,f.Locks,[]byte(f.Report),[]byte(f.Calibration),f.Params,image.Color.ICCSHA256);if err!=nil{t.Fatal(err)}
 capacity,err:=NegativeResources(image.Canvas.Width,image.Canvas.Height,image.Alpha!=nil);if err!=nil{t.Fatal(err)}
 return f.Params,source,qualified,capacity
}

func TestNegativeDraftStableIdentityOwnedRecoveryAndConflictBoundary(t *testing.T){
 params,source,qualified,capacity:=negativeDraftFixture(t)
 body,_,err:=params.Compile();if err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,project,snapshot:=uuid.New(),uuid.New(),uuid.New()
 compile:=func(who auth.Identity,snapshot uuid.UUID,key string)NegativeDraft{
  draft,err:=CompileNegativeDraft(context.Background(),who,tenant,project,snapshot,key,body,source,qualified,capacity);if err!=nil{t.Fatal(err)};return draft
 }
 first:=compile(who,snapshot,"negative-draft-key-0001")
 replay:=compile(who,snapshot,"negative-draft-key-0001")
 if first.raw!=replay.raw{t.Fatal("identical draft replay changed bytes")}
 s,err:=first.Snapshot();if err!=nil{t.Fatal(err)}
 changedSnapshot,_:=compile(who,uuid.New(),"negative-draft-key-0001").Snapshot()
 if changedSnapshot.Owner.RunID!=s.Owner.RunID||changedSnapshot.RequestSHA==s.RequestSHA{t.Fatal("idempotency conflict not detectable")}
 changedKey,_:=compile(who,snapshot,"negative-draft-key-0002").Snapshot()
 if changedKey.Owner.RunID==s.Owner.RunID{t.Fatal("different key reused run")}
 changedActor:=who;changedActor.UserID=uuid.New()
 actor,_:=compile(changedActor,snapshot,"negative-draft-key-0001").Snapshot()
 if actor.Owner.RunID==s.Owner.RunID{t.Fatal("different actor reused run")}
 plan,ref,restored,err:=first.RestorePlan(context.Background(),qualified)
 if err!=nil||ref.ArtifactID!=s.ParamsDocument||ref.SHA256!=s.ParamsSHA||!bytes.Equal(plan.Params,body)||!bytes.Equal(restored,source){t.Fatal("draft lost owned originals",err)}
 plan.Params[0]='[';restored[0]='[';s.RecoveryJCS="changed"
 if _,_,_,err:=first.RestorePlan(context.Background(),qualified);err!=nil{t.Fatal("returned mutable data corrupted draft",err)}
}

func TestNegativeDraftRestoreRejectsTamperedEnvelope(t *testing.T){
 params,source,qualified,capacity:=negativeDraftFixture(t);body,_,_:=params.Compile()
 draft,err:=CompileNegativeDraft(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),uuid.New(),uuid.New(),"negative-draft-key-0001",body,source,qualified,capacity);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"owner","params","request","inventory","recovery","reserved"}{
  t.Run(mode,func(t *testing.T){
   s,_:=draft.Snapshot()
   switch mode{case "owner":s.Owner.ProjectID=uuid.New();case "params":s.ParamsDocument=uuid.New();case "request":s.RequestSHA="bad";case "inventory":s.InputDigest="bad";case "recovery":s.RecoveryJCS="{}";case "reserved":s.ReservedBytes++}
   raw,err:=canonical.Marshal(s);if err!=nil{t.Fatal(err)}
   if _,_,_,err:=(NegativeDraft{raw:string(raw)}).RestorePlan(context.Background(),qualified);err==nil{t.Fatal("tampered draft restored")}
  })
 }
}

func TestNegativeDraftCancellationAndMissingScope(t *testing.T){
 params,source,qualified,capacity:=negativeDraftFixture(t);body,_,_:=params.Compile()
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=CompileNegativeDraft(ctx,auth.Identity{},uuid.Nil,uuid.Nil,uuid.Nil,"",body,source,qualified,capacity);err!=context.Canceled{t.Fatal("cancellation lost",err)}
 if _,err:=CompileNegativeDraft(context.Background(),auth.Identity{},uuid.New(),uuid.New(),uuid.New(),"negative-draft-key-0001",body,source,qualified,capacity);err==nil{t.Fatal("missing actor accepted")}
}


func TestNegativeDraftPersistenceDecoderRequiresExactOwnerAndCanonicalEnvelope(t *testing.T){
 params,source,qualified,capacity:=negativeDraftFixture(t);body,_,_:=params.Compile()
 draft,err:=CompileNegativeDraft(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),uuid.New(),uuid.New(),"negative-draft-key-0001",body,source,qualified,capacity);if err!=nil{t.Fatal(err)}
 snapshot,_:=draft.Snapshot()
 for _,mode:=range []string{"valid","owner","unknown","whitespace","unqualified"}{
  t.Run(mode,func(t *testing.T){
   raw:=[]byte(draft.raw);expected:=snapshot.Owner;q:=qualified
   switch mode{
   case "owner":expected.TenantID=uuid.New()
   case "unknown":var doc map[string]any;if json.Unmarshal(raw,&doc)!=nil{t.Fatal("fixture")};doc["unexpected"]=true;raw,_=canonical.Marshal(doc)
   case "whitespace":raw=append([]byte(" "),raw...)
   case "unqualified":q=VerifiedNegativeQualification{}
   }
   restored,err:=RestoreNegativeDraft(context.Background(),raw,expected,q)
   if mode=="valid"{
    if err!=nil||restored.raw!=draft.raw{t.Fatal("valid saved draft refused",err)}
    raw[0]='['
    if _,_,_,err:=restored.RestorePlan(context.Background(),qualified);err!=nil{t.Fatal("caller buffer corrupted saved draft",err)}
   }else if err==nil{t.Fatal("invalid persistence evidence accepted",mode)}
  })
 }
}
