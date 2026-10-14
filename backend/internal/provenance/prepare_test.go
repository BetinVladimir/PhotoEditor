package provenance

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
type signingSourceStub struct{evidence SigningExportEvidence;calls int;change bool;err error}
func(s *signingSourceStub)ResolveSigningExport(context.Context,auth.Identity,uuid.UUID,uuid.UUID)(SigningExportEvidence,error){s.calls++;value:=s.evidence;if s.change&&s.calls>1{value.Export.ArtifactID=uuid.New()};return value,s.err}
func TestSigningPreparationPinsSourceAndOwnsFrozenBytes(t *testing.T){
 config,params,now:=signingRegistryFixture(t)
 registry,err:=NewSigningPolicyRegistry(config,&signingAuthorityStub{});if err!=nil{t.Fatal(err)}
 body,lineage:=contractBytes(t,SigningLineage{Schema:1,SourceHashes:[]string{params.Export.SHA256},Actions:[]SigningAction{}})
 tid,export,project:=uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 source:=&signingSourceStub{evidence:SigningExportEvidence{Project:project,ExportID:export,Export:params.Export,MIME:"image/jpeg",Lineage:lineage,LineageBody:body}}
 first,err:=PrepareSigningIntent(context.Background(),who,tid,export,params.PolicyID,"key",source,registry,now)
 if err!=nil||first.ProjectID()!=project||source.calls!=2{t.Fatal("preparation failed",err)}
 second,err:=PrepareSigningIntent(context.Background(),who,tid,export,params.PolicyID,"key",source,registry,now)
 if err!=nil||first.WorkflowID()!=second.WorkflowID()||first.TaskID()!=second.TaskID()||first.RequestSHA()!=second.RequestSHA(){t.Fatal("unstable replay identity",err)}
 decoded,err:=DecodeSignParams(first.ParamsBytes());if err!=nil||decoded.Export!=params.Export||decoded.Lineage!=lineage||decoded.Policy!=params.Policy{t.Fatal("params lost pins",err)}
 copy:=first.ParamsBytes();copy[0]='x';body[0]='x'
 if first.ParamsBytes()[0]!='{'||first.LineageBytes()[0]!='{'{t.Fatal("frozen bytes aliased")}
}
func TestSigningPreparationRefusesStaleAuthorityBeforeExposingIntent(t *testing.T){
 for _,mode:=range []string{"change","denied","mime","lineage","cancelled","key"}{t.Run(mode,func(t *testing.T){
  config,params,now:=signingRegistryFixture(t);registry,err:=NewSigningPolicyRegistry(config,&signingAuthorityStub{});if err!=nil{t.Fatal(err)}
  body,lineage:=contractBytes(t,SigningLineage{Schema:1,SourceHashes:[]string{params.Export.SHA256},Actions:[]SigningAction{}})
  export:=uuid.New();source:=&signingSourceStub{evidence:SigningExportEvidence{Project:uuid.New(),ExportID:export,Export:params.Export,MIME:"image/png",Lineage:lineage,LineageBody:body}}
  ctx:=context.Background();key:="key";cause:=errors.New("permission revoked")
  switch mode{case "change":source.change=true;case "denied":source.err=cause;case "mime":source.evidence.MIME="image/tiff";case "lineage":source.evidence.LineageBody=[]byte("{}");case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel();case "key":key="bad key"}
  frozen,err:=PrepareSigningIntent(ctx,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),export,params.PolicyID,key,source,registry,now)
  if err==nil||frozen.WorkflowID()!=uuid.Nil{t.Fatal("invalid evidence exposed intent")}
  if mode=="denied"&&!errors.Is(err,cause){t.Fatal("authority error lost")}
  if (mode=="cancelled"||mode=="key")&&source.calls!=0{t.Fatal("invalid preflight reached source")}
 })}
}
