package interchange

import (
 "bytes"
 "context"
 "crypto"
 "crypto/ed25519"
 "crypto/rand"
 "encoding/base64"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

const historicalOriginDomain="photoeditor:historical-origin:v1\x00"
// HistoricalOriginPayload authenticates an import-local assertion, never an
// archive assertion. Original and prepared identities are deliberately distinct.
type HistoricalOriginPayload struct {
 Schema string `json:"schema"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Workflow uuid.UUID `json:"workflow_id"`
 Run uuid.UUID `json:"run_id"`
 Entry uuid.UUID `json:"entry_id"`
 Target v2input.SessionVersionBinding `json:"target"`
 OriginalSource v2input.Ref `json:"original_source"`
 Contract string `json:"contract"`
 Original v2input.Ref `json:"original"`
 Prepared v2input.Ref `json:"prepared"`
 OriginalImage v2input.Ref `json:"original_image"`
 OriginalBoxes *v2input.Ref `json:"original_boxes"`
 Model string `json:"model_lock_id"`
 Backend string `json:"backend"`
 Mode string `json:"producer_mode"`
}
// Payload is base64 JCS, so original archive refs are metadata rather than
// executable input-closure edges. Prepared refs must independently be inputs.
type HistoricalOriginReceipt struct {
 Schema string `json:"schema"`
 Issuer uuid.UUID `json:"issuer_id"`
 Key string `json:"key_id"`
 Payload string `json:"payload"`
 Signature string `json:"signature"`
}
type HistoricalOriginIssuer struct {
 Authority ImportHistoricalNativeAuthority
 Project uuid.UUID
 Issuer uuid.UUID
 KeyID string
 Signer crypto.Signer
}
func(i HistoricalOriginIssuer)checkScope(ctx context.Context)error{
 q:=i.Authority
 if q.Catalog.Store.DB==nil{return v2input.ErrInvalid}
 return database.TenantTransaction(ctx,q.Catalog.Store.DB,q.Catalog.Scope.Tenant,func(tx pgx.Tx)error{
  if err:=q.Catalog.Store.checkPreparedRun(ctx,tx);err!=nil{return err}
  var raw []byte
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_import_sources($1,$2,$3)`,q.Catalog.Scope.Tenant,q.Catalog.Scope.Workflow,string(q.Catalog.Store.Parsed)).Scan(&raw);err!=nil{return err}
  var sources struct{Project uuid.UUID `json:"project_id"`}
  if json.Unmarshal(raw,&sources)!=nil||sources.Project!=i.Project{return v2input.ErrInvalid};return ctx.Err()
 })
}
func validateHistoricalOriginPayload(p HistoricalOriginPayload)error{
 if p.Schema!="historical-origin-payload/v1"||p.Mode!="local_canonical"||p.Tenant==uuid.Nil||p.Project==uuid.Nil||p.Workflow==uuid.Nil||p.Run==uuid.Nil||p.Entry==uuid.Nil||p.Target.AssetVersionID==uuid.Nil||!validImportRef(p.Target.Source)||!validImportRefForSHA(p.Target.CanvasHash)||!validImportRef(p.OriginalSource)||!validImportRef(p.Original)||!validImportRef(p.Prepared)||!validImportRef(p.OriginalImage){return v2input.ErrInvalid}
 switch p.Contract{case "WorkingImage":if p.Original!=p.OriginalImage||p.OriginalBoxes!=nil||p.Model!=""||p.Backend!=""{return v2input.ErrInvalid};case "FaceBoxes","FaceLandmarks":if !validImportRefForSHA(p.Model)||(p.Backend!="cpu"&&p.Backend!="mps"&&p.Backend!="cuda"){return v2input.ErrInvalid};if (p.Contract=="FaceLandmarks")!=(p.OriginalBoxes!=nil){return v2input.ErrInvalid};if p.OriginalBoxes!=nil&&!validImportRef(*p.OriginalBoxes){return v2input.ErrInvalid};default:return v2input.ErrInvalid};return nil
}
// Issue never uses the foreign fallback. A local certificate requires current
// canonical producer SQL authority both before and after signing, plus exact
// mechanical prepared-byte replay. Private-key possession alone is insufficient.
func(i HistoricalOriginIssuer)Issue(ctx context.Context,kind string,prepared v2input.Ref,raw []byte)(HistoricalOriginReceipt,error){
 var zero HistoricalOriginReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if i.Signer==nil||i.Issuer==uuid.Nil||i.Project==uuid.Nil||len(i.KeyID)<1||len(i.KeyID)>120||!validImportRef(prepared)||rawSHA(raw)!=prepared.SHA256{return zero,v2input.ErrInvalid}
 public,ok:=i.Signer.Public().(ed25519.PublicKey);if !ok||len(public)!=ed25519.PublicKeySize{return zero,v2input.ErrInvalid}
 if err:=i.checkScope(ctx);err!=nil{return zero,err}
 authority:=i.Authority;authority.Foreign=nil;authority.source=prepared
 original,originalRaw,err:=authority.original(ctx,kind);if err!=nil{return zero,err}
 if err=authority.QualifyPreparedHistoricalDocument(ctx,kind,raw,authority.Target);err!=nil{return zero,err}
 expected,err:=authority.preparedRef(ctx,original,kind);if err!=nil||expected!=prepared{return zero,v2input.ErrInvalid}
 payload:=HistoricalOriginPayload{Schema:"historical-origin-payload/v1",Tenant:authority.Catalog.Scope.Tenant,Project:i.Project,Workflow:authority.Catalog.Scope.Workflow,Run:authority.Catalog.Scope.Run,Entry:authority.Catalog.Scope.Entry,Target:authority.Target,OriginalSource:authority.Old.Source,Contract:kind,Original:original,Prepared:prepared,Mode:"local_canonical"}
 payload.OriginalImage=original
 if kind!="WorkingImage"{var header struct{Image v2input.Ref `json:"image"`;Faces json.RawMessage `json:"faces"`;Model string `json:"model_lock_id"`;Backend string `json:"backend"`};if json.Unmarshal(originalRaw,&header)!=nil{return zero,v2input.ErrInvalid};payload.OriginalImage=header.Image;payload.Model=header.Model;payload.Backend=header.Backend;if kind=="FaceLandmarks"{var boxes v2input.Ref;if json.Unmarshal(header.Faces,&boxes)!=nil{return zero,v2input.ErrInvalid};payload.OriginalBoxes=&boxes}}
 if err=validateHistoricalOriginPayload(payload);err!=nil{return zero,err}
 body,err:=canonical.Marshal(payload);if err!=nil{return zero,err};message:=append([]byte(historicalOriginDomain),body...)
 signature,err:=i.Signer.Sign(rand.Reader,message,crypto.Hash(0));if err!=nil{return zero,err}
 if !ed25519.Verify(public,message,signature){return zero,v2input.ErrInvalid}
 if err=authority.QualifyPreparedHistoricalDocument(ctx,kind,append([]byte(nil),raw...),authority.Target);err!=nil{return zero,err}
 if err=i.checkScope(ctx);err!=nil{return zero,err}
 return HistoricalOriginReceipt{Schema:"historical-origin-receipt/v1",Issuer:i.Issuer,Key:i.KeyID,Payload:base64.StdEncoding.EncodeToString(body),Signature:base64.StdEncoding.EncodeToString(signature)},ctx.Err()
}
func VerifyHistoricalOriginReceipt(raw []byte,issuer uuid.UUID,keyID string,public ed25519.PublicKey)(HistoricalOriginPayload,error){
 var zero HistoricalOriginPayload;var receipt HistoricalOriginReceipt
 if len(raw)<2||len(raw)>65536||len(public)!=ed25519.PublicKeySize||decodeImportDocument(raw,&receipt)!=nil||receipt.Schema!="historical-origin-receipt/v1"||receipt.Issuer!=issuer||issuer==uuid.Nil||receipt.Key!=keyID||keyID==""{return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return zero,v2input.ErrInvalid}
 payload,err:=base64.StdEncoding.Strict().DecodeString(receipt.Payload);if err!=nil||base64.StdEncoding.EncodeToString(payload)!=receipt.Payload{return zero,v2input.ErrInvalid}
 signature,err:=base64.StdEncoding.Strict().DecodeString(receipt.Signature);if err!=nil||len(signature)!=ed25519.SignatureSize||base64.StdEncoding.EncodeToString(signature)!=receipt.Signature{return zero,v2input.ErrInvalid}
 normalized,err=canonical.Bytes(payload);if err!=nil||!bytes.Equal(normalized,payload)||!ed25519.Verify(public,append([]byte(historicalOriginDomain),payload...),signature){return zero,v2input.ErrInvalid}
 if decodeImportDocument(payload,&zero)!=nil||validateHistoricalOriginPayload(zero)!=nil{return HistoricalOriginPayload{},v2input.ErrInvalid};return zero,nil
}
