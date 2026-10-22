package interchange

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type conservativeImportAuthority struct{calls int;denyAt int;bindings []v2input.SessionVersionBinding}
func(a *conservativeImportAuthority)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 a.calls++;a.bindings=append(a.bindings,binding)
 if a.calls==a.denyAt{return v2input.ErrInvalid}
 if operation!="defects.auto_repair"||version!="1"{return v2input.ErrInvalid}
 return ctx.Err()
}

func TestImportConservativeDefectParamsRebindsAllDependencies(t *testing.T){
 ref:=func(s string)v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(s))}}
 old:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref("image"),CanvasHash:rawSHA([]byte("canvas"))}
 target:=old;target.AssetVersionID=uuid.New();target.Source.ArtifactID=uuid.New()
 proposal,report,resolved:=ref("proposal"),ref("report"),ref("resolved")
 raw,err:=canonical.Marshal(map[string]any{"policy":"conservative_auto_v1","image":old.Source,"proposal":proposal,"proposal_report":report,"resolved":resolved});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","archive_denied","target_denied","missing_report","unchanged_ref","canvas","cancelled"}{t.Run(mode,func(t *testing.T){
  authority:=&conservativeImportAuthority{}
  if mode=="archive_denied"{authority.denyAt=1};if mode=="target_denied"{authority.denyAt=2}
  codec:=ImportConservativeParamsCodec{Qualifier:authority}
  currentTarget:=target;if mode=="canvas"{currentTarget.CanvasHash=rawSHA([]byte("other"))}
  var contracts []string
  missing:=errors.New("report contract unavailable")
  resolve:=func(original v2input.Ref,kind string)(v2input.Ref,error){
   contracts=append(contracts,kind)
   if mode=="missing_report"&&kind=="DefectProposeReport"{return v2input.Ref{},missing}
   if mode=="unchanged_ref"{return original,nil}
   return v2input.Ref{ArtifactID:uuid.New(),SHA256:original.SHA256},nil
  }
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  prepared,err:=codec.RemapImportDocument(ctx,"Params",raw,old,currentTarget,resolve)
  if mode!="valid"{
   if err==nil||prepared!=nil{t.Fatal("unqualified import returned Params")}
   if mode=="missing_report"&&!errors.Is(err,missing){t.Fatal("dependency error lost",err)}
   if (mode=="archive_denied"||mode=="canvas"||mode=="cancelled")&&len(contracts)!=0{t.Fatal("dependencies resolved before admission")}
   return
  }
  want:=[]string{"DefectCandidates","DefectProposeReport","ResolvedStyle"}
  if len(contracts)!=3||authority.calls!=2||authority.bindings[0]!=old||authority.bindings[1]!=target{t.Fatal("authority or closure missing")}
  for i:=range want{if contracts[i]!=want[i]{t.Fatal(contracts)}}
  var fields map[string]json.RawMessage;if json.Unmarshal(prepared,&fields)!=nil{t.Fatal("invalid prepared Params")}
  var image v2input.Ref;if json.Unmarshal(fields["image"],&image)!=nil||image!=target.Source{t.Fatal("image not rebound")}
  if fields["review"]!=nil{t.Fatal("fabricated human review")}
 })}
}
