package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type exportCorrectionEvidenceStub struct{evidence ExportCorrectionProducerEvidence;calls int;changeAfter int}
func(s *exportCorrectionEvidenceStub)LookupExportCorrectionEvidence(ctx context.Context,entry v2input.Entry,raw []byte)(ExportCorrectionProducerEvidence,error){
 s.calls++;if err:=ctx.Err();err!=nil{return ExportCorrectionProducerEvidence{},err}
 value:=s.evidence;if s.changeAfter>0&&s.calls>s.changeAfter{value.Version="changed"};return value,nil
}
func TestExportCorrectionProducerReadsCanonicalSceneRootAndRechecks(t *testing.T){
 for _,mode:=range []string{"valid","edge","group","digest","ordinals","root_hash","changed","authority","cancelled"}{t.Run(mode,func(t *testing.T){
  binding:=func()v2input.SessionVersionBinding{return v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("working"))},CanvasHash:rawSHA([]byte("canvas"))}}
  digest,policy:=rawSHA([]byte("snapshot")),rawSHA([]byte("policy"))
  items:=[]any{}
  for _,ordinal:=range []int{2,7}{items=append(items,map[string]any{"binding":binding(),"ordinal":ordinal,"delta":v2draft.CorrectionVector{},"disposition":"no_change","reason":nil})}
  raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":digest,"items":items});if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"CorrectionPlan"}
  group,snapshot:=uuid.New(),uuid.New()
  selected:=map[string]any{"id":group,"ordinals":[]int{2,7},"reference_ordinal":2,"confidence":0.9,"corrections":entry.Ref}
  other:=map[string]any{"id":uuid.New(),"ordinals":[]int{0,1,3,4,5,6},"reference_ordinal":0,"confidence":0.9,"corrections":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("other corrections"))}}
  scene:=map[string]any{"schema_version":1,"snapshot_id":snapshot,"snapshot_digest":digest,"policy_sha":policy,"groups":[]any{other,selected}}
  switch mode{
  case "edge":selected["corrections"]=v2input.Ref{ArtifactID:uuid.New(),SHA256:entry.Ref.SHA256}
  case "group":selected["id"]=uuid.New()
  case "digest":scene["snapshot_digest"]=rawSHA([]byte("wrong"))
  case "ordinals":selected["ordinals"]=[]int{7,2}
  }
  root,err:=canonical.Marshal(scene);if err!=nil{t.Fatal(err)}
  evidence:=ExportCorrectionProducerEvidence{Group:group,Snapshot:snapshot,Digest:digest,Policy:policy,Count:8,Root:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(root)},Bucket:"bucket",Key:"root",Version:"v1",Bytes:int64(len(root)),MIME:"application/json"}
  source:=&exportCorrectionEvidenceStub{evidence:evidence}
  objects:=&importReaderStub{body:root}
  fallback:=&exportSemanticsStub{deny:mode=="authority"}
  semantics:=ExportCorrectionProducerSemantics{Source:source,Objects:objects,Fallback:fallback}
  ctx:=context.Background()
  if mode=="root_hash"{objects.body=[]byte(`{}`)}
  if mode=="changed"{source.changeAfter=2}
  if mode=="cancelled"{var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  err=semantics.QualifyExportDependency(ctx,entry,raw,nil)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected producer qualification: %v",err)}
  expected:=0;if mode=="valid"||mode=="authority"{expected=1}
  if fallback.calls!=expected{t.Fatal("unqualified producer reached fallback")}
  if mode=="valid"&&(source.calls!=3||objects.opens!=1){t.Fatal("missing post-IO evidence recheck")}
 })}
}
