package interchange

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestApprovalDependencyDecoderRejectsPartialOrAmbiguousScope(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))}
 for _,mode:=range []string{"valid","empty","duplicate","binary","hash","unknown_field"}{t.Run(mode,func(t *testing.T){
  entries:=[]v2input.Entry{{Ref:ref,Contract:"StyleV2"}}
  switch mode{case "empty":entries=nil;case "duplicate":entries=append(entries,entries[0]);case "binary":entries[0].Contract="StyleBackgroundAsset";case "hash":entries[0].Ref.SHA256="invalid"}
  raw,err:=canonical.Marshal(entries);if err!=nil{t.Fatal(err)}
  if mode=="unknown_field"{raw=[]byte(`[{"ref":{"artifact_id":"`+ref.ArtifactID.String()+`","sha256":"`+ref.SHA256+`"},"contract":"StyleV2","extra":true}]`)}
  got,err:=decodeImportApprovalDependencies(raw)
  if mode=="valid"{if err!=nil||len(got)!=1||got[0].Ref!=ref{t.Fatal("lost exact reference",err)};return}
  if err==nil{t.Fatal("accepted invalid scope")}
 })}
}

func TestApprovalDependenciesAcceptPostgresJSONBTextWithoutRelaxingFields(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))}
 raw:=[]byte(`[ { "ref": { "sha256": "`+ref.SHA256+`", "artifact_id": "`+ref.ArtifactID.String()+`" }, "contract": "StyleV2" } ]`)
 entries,err:=decodeImportApprovalDependencies(raw)
 if err!=nil||len(entries)!=1||entries[0].Ref!=ref{t.Fatal("rejected SQL jsonb serialization",err)}
}
