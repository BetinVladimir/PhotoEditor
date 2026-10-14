package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Registration is prepared before DecideImport and must be persisted only in
// its approval transaction. Preparing it grants neither approval nor render ACL.
type ImportStyleRegistration struct {
 StyleID uuid.UUID
 Config v2input.Ref
 Original v2input.Ref
 Body []byte
 Background *ImportBackgroundRegistration
}
type ImportBackgroundRegistration struct {
 PortableID string
 Artifact v2input.Ref
 Occurrence ImportBackgroundSource
}
func CompileImportStyleRegistration(ctx context.Context,scope ImportPreparedScope,original,prepared v2input.Ref,raw []byte,background *ImportBackgroundSource,qualifier ImportStyleQualifier)(ImportStyleRegistration,error){
 invalid:=func()(ImportStyleRegistration,error){return ImportStyleRegistration{},v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return ImportStyleRegistration{},err}
 if qualifier==nil||scope.Tenant==uuid.Nil||scope.Run==uuid.Nil||scope.Workflow==uuid.Nil||scope.Entry==uuid.Nil||scope.Binding.AssetVersionID==uuid.Nil||!validImportRef(scope.Binding.Source)||!validImportRef(v2input.Ref{ArtifactID:scope.Entry,SHA256:scope.Binding.CanvasHash})||!validImportRef(original)||!validImportRef(prepared)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=prepared.SHA256{return invalid()}
 if prepared.ArtifactID!=uuid.NewSHA1(scope.Workflow,[]byte("interchange:dependency:"+scope.Entry.String()+":StyleV2:"+original.ArtifactID.String())){return invalid()}
 body:=append([]byte(nil),raw...)
 normalized,err:=canonical.Bytes(body);if err!=nil||!bytes.Equal(normalized,body){return invalid()}
 if _,err:=v2draft.DecodeStyleBaseline(body);err!=nil{return ImportStyleRegistration{},err}
 var fields struct{StyleID uuid.UUID `json:"style_id"`;Extensions struct{Background *struct{Mode string `json:"mode"`;AssetID string `json:"asset_id"`} `json:"background"`} `json:"extensions"`}
 if err:=json.Unmarshal(body,&fields);err!=nil{return ImportStyleRegistration{},err}
 expected:=uuid.NewSHA1(scope.Binding.AssetVersionID,[]byte("interchange:style:"+original.SHA256))
 if fields.StyleID!=expected{return invalid()}
 out:=ImportStyleRegistration{StyleID:expected,Config:prepared,Original:original,Body:body}
 assetMode:=fields.Extensions.Background!=nil&&fields.Extensions.Background.Mode=="asset"
 if assetMode{
  if background==nil{return invalid()};owned:=*background
  if owned.Style!=original{return invalid()}
  local:=v2input.Ref{ArtifactID:importBackgroundID(scope.Workflow,owned,scope.Binding),SHA256:owned.Original.SHA256}
  certificate:=scope;certificate.Contract="StyleBackgroundAsset";certificate.Warp=nil;certificate.Background=&owned
  if err:=validateImportBackgroundOccurrence(certificate,local);err!=nil{return ImportStyleRegistration{},err}
  if fields.Extensions.Background.AssetID!=local.ArtifactID.String(){return invalid()}
  out.Background=&ImportBackgroundRegistration{PortableID:local.ArtifactID.String(),Artifact:local,Occurrence:owned}
 }else if background!=nil{return invalid()}
 if err:=qualifier.QualifyImportedStyle(ctx,append([]byte(nil),body...),scope.Binding);err!=nil{return ImportStyleRegistration{},err}
 return out,ctx.Err()
}
