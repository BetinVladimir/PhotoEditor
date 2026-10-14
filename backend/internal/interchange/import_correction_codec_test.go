package interchange

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type correctionSourceStub struct{pairs []ImportCorrectionBinding;calls int;fail error}
func(s *correctionSourceStub)QualifyImportRecipeSource(_ context.Context,entry uuid.UUID,old,target v2input.SessionVersionBinding)error{
 s.calls++;if s.fail!=nil{return s.fail}
 for _,item:=range s.pairs{if item.Entry==entry&&item.Old==old&&item.New==target{return nil}}
 return v2input.ErrInvalid
}
func correctionBindingFixture()ImportCorrectionBinding{
 binding:=func()v2input.SessionVersionBinding{return v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))},CanvasHash:rawSHA([]byte("canvas"))}}
 return ImportCorrectionBinding{Entry:uuid.New(),Old:binding(),New:binding()}
}
func TestCorrectionCodecPreservesAllRowsAndAuthoredValues(t *testing.T){
 mappings:=[]ImportCorrectionBinding{correctionBindingFixture(),correctionBindingFixture()}
 rows:=[]map[string]any{
  {"binding":mappings[0].Old,"ordinal":0,"delta":v2draft.CorrectionVector{EV:.25,LogR:.03},"disposition":"apply","reason":nil},
  {"binding":mappings[1].Old,"ordinal":1,"delta":v2draft.CorrectionVector{},"disposition":"manual_locked","reason":"human_lock"},
 }
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":rawSHA([]byte("archived snapshot")),"items":rows});if err!=nil{t.Fatal(err)}
 source:=&correctionSourceStub{pairs:append([]ImportCorrectionBinding(nil),mappings...)}
 snapshot:=rawSHA([]byte("current snapshot"))
 codec,err:=NewImportCorrectionPlanCodec(snapshot,mappings,source,nil);if err!=nil{t.Fatal(err)}
 prepared,err:=codec.RemapImportDocument(context.Background(),"CorrectionPlan",raw,mappings[0].Old,mappings[0].New,nil);if err!=nil{t.Fatal(err)}
 if err:=v2draft.ValidateCorrectionPlan(prepared,mappings[0].New);err!=nil{t.Fatal(err)}
 var result struct{Snapshot string `json:"snapshot_digest"`;Items []map[string]json.RawMessage `json:"items"`};if err:=json.Unmarshal(prepared,&result);err!=nil{t.Fatal(err)}
 if result.Snapshot!=snapshot||len(result.Items)!=2||source.calls<4{t.Fatal("incomplete mapping/authority recheck")}
 for i,item:=range result.Items{
  var binding v2input.SessionVersionBinding;if err:=json.Unmarshal(item["binding"],&binding);err!=nil||binding!=mappings[i].New{t.Fatal("wrong binding",err)}
  for _,field:=range []string{"ordinal","delta","disposition","reason"}{expected,err:=canonical.Marshal(rows[i][field]);if err!=nil||string(item[field])!=string(expected){t.Fatal("authored field changed",field,err)}}
 }
 // Caller changes to the installation slice cannot rewrite pinned mappings.
 mappings[0].New.AssetVersionID=uuid.New()
 if err:=codec.QualifyRemappedImportDocument(context.Background(),"CorrectionPlan",prepared,codec.bindings[mappings[0].Old.AssetVersionID].New);err!=nil{t.Fatal("mapping aliases caller",err)}
}
func TestCorrectionCodecRejectsMissingSourcesRevocationAndChangedSnapshot(t *testing.T){
 first,second:=correctionBindingFixture(),correctionBindingFixture()
 rows:=[]any{}
 for i,item:=range []ImportCorrectionBinding{first,second}{rows=append(rows,map[string]any{"binding":item.Old,"ordinal":i,"delta":v2draft.CorrectionVector{},"disposition":"no_change","reason":nil})}
 raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":rawSHA([]byte("snapshot")),"items":rows});if err!=nil{t.Fatal(err)}
 source:=&correctionSourceStub{pairs:[]ImportCorrectionBinding{first,second}}
 codec,err:=NewImportCorrectionPlanCodec(rawSHA([]byte("current")),[]ImportCorrectionBinding{first},source,nil);if err!=nil{t.Fatal(err)}
 if _,err:=codec.RemapImportDocument(context.Background(),"CorrectionPlan",raw,first.Old,first.New,nil);err==nil{t.Fatal("nonimported source silently removed")}else{var unsupported *ImportRecipeUnsupported;if !errors.As(err,&unsupported)||unsupported.Reason!="correction_source_not_imported"{t.Fatal("missing source diagnostic",err)}}
 codec,err=NewImportCorrectionPlanCodec(rawSHA([]byte("current")),[]ImportCorrectionBinding{first,second},source,nil);if err!=nil{t.Fatal(err)}
 denied:=errors.New("source access revoked");source.fail=denied
 if _,err:=codec.RemapImportDocument(context.Background(),"CorrectionPlan",raw,first.Old,first.New,nil);!errors.Is(err,denied){t.Fatal("revocation reclassified",err)}
 source.fail=nil
 prepared,err:=codec.RemapImportDocument(context.Background(),"CorrectionPlan",raw,first.Old,first.New,nil);if err!=nil{t.Fatal(err)}
 var document map[string]json.RawMessage;if err:=json.Unmarshal(prepared,&document);err!=nil{t.Fatal(err)};document["snapshot_digest"],_=canonical.Marshal(rawSHA([]byte("foreign")));changed,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
 if err:=codec.QualifyRemappedImportDocument(context.Background(),"CorrectionPlan",changed,first.New);err==nil{t.Fatal("foreign snapshot accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=codec.RemapImportDocument(ctx,"CorrectionPlan",raw,first.Old,first.New,nil);!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
