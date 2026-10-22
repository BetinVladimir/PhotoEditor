package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// A foreign verifier is process-installed and must authenticate original signed
// producer receipts independently. Absence never falls back to archive claims.
type ImportHistoricalForeignAuthority interface{QualifyForeignHistoricalDocument(context.Context,string,v2input.Ref,[]byte,v2input.SessionVersionBinding,v2input.SessionVersionBinding)error}
type ImportHistoricalNativeAuthority struct{
 Catalog SQLPreparedImportCatalog
 Reader ImportArchiveDocumentReader
 Masters ImportMasterReader
 Sources ImportRecipeSourceQualifier
 Old,Target v2input.SessionVersionBinding
 Foreign ImportHistoricalForeignAuthority
 source v2input.Ref
}
func(q ImportHistoricalNativeAuthority)BindHistoricalDocumentSource(ref v2input.Ref)ImportHistoricalDocumentAuthority{if !validImportRef(ref){return nil};q.source=ref;return q}
func(q ImportHistoricalNativeAuthority)original(ctx context.Context,kind string)(v2input.Ref,[]byte,error){
 if q.Reader==nil||q.Catalog.Store.DB==nil||!validImportRef(q.source){return v2input.Ref{},nil,v2input.ErrInvalid}
 var parsed importParsedDocument;if decodeImportDocument(q.Catalog.Store.Parsed,&parsed)!=nil{return v2input.Ref{},nil,v2input.ErrInvalid}
 files:=map[string]v2input.Ref{};for _,f:=range parsed.Files{files[f.Path]=f.Content}
 found:=0;var ref v2input.Ref;var staged v2input.Ref
 for _,item:=range parsed.Manifest.Inventory{
  mapped:=uuid.NewSHA1(q.Catalog.Scope.Workflow,[]byte("interchange:dependency:"+q.Catalog.Scope.Entry.String()+":"+kind+":"+item.ID.String()))
  if item.Contract==kind&&(q.source.ArtifactID==item.ID||q.source.ArtifactID==mapped){found++;ref=v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA};staged=files[item.Path]}
 }
 if found!=1||!validImportRef(staged)||staged.SHA256!=ref.SHA256{return v2input.Ref{},nil,v2input.ErrInvalid}
 raw,err:=q.Reader.ReadImportArchiveDocument(ctx,staged,1<<20);if err!=nil{return v2input.Ref{},nil,err};if rawSHA(raw)!=ref.SHA256{return v2input.Ref{},nil,v2input.ErrInvalid};return ref,append([]byte(nil),raw...),nil
}
func(q ImportHistoricalNativeAuthority)readOriginalImage(ctx context.Context,ref v2input.Ref)([]byte,error){child:=q;child.source=ref;actual,raw,err:=child.original(ctx,"WorkingImage");if err!=nil{return nil,err};if actual!=ref{return nil,v2input.ErrInvalid};return raw,nil}
func(q ImportHistoricalNativeAuthority)QualifyArchivedHistoricalDocument(ctx context.Context,kind string,raw []byte,old,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Sources==nil||q.Masters==nil||old!=q.Old||target!=q.Target{return v2input.ErrInvalid}
 ref,original,err:=q.original(ctx,kind);if err!=nil{return err};if !bytes.Equal(original,raw){return v2input.ErrInvalid}
 if err:=q.Sources.QualifyImportRecipeSource(ctx,q.Catalog.Scope.Entry,old,target);err!=nil{return err}
 imageRef:=ref;imageRaw:=raw
 var header struct{Image v2input.Ref `json:"image"`;Faces json.RawMessage `json:"faces"`;Model string `json:"model_lock_id"`;Backend string `json:"backend"`}
 if kind!="WorkingImage"{if json.Unmarshal(raw,&header)!=nil||!validImportRef(header.Image){return v2input.ErrInvalid};imageRef=header.Image;imageRaw,err=q.readOriginalImage(ctx,imageRef);if err!=nil{return err}}
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return err};source:=image.Source(review.ArtifactRef{ID:imageRef.ArtifactID,SHA256:imageRef.SHA256})
 if kind=="FaceBoxes"{if _,err=review.ParseFaceBoxes(raw,source);err!=nil{return err}}else if kind=="FaceLandmarks"{if _,err=review.ParseFaceLandmarkCollection(raw,source);err!=nil{return err}}else if kind!="WorkingImage"{return v2input.ErrInvalid}
 current,err:=q.Masters.ReadImportMaster(ctx,target.Source,1<<20);if err!=nil{return err};if rawSHA(current)!=target.Source.SHA256{return v2input.ErrInvalid};if err=validateImportHistoricalImageGeometry(imageRaw,current);err!=nil{return err}
 var boxes v2input.Ref;if kind=="FaceLandmarks"{if json.Unmarshal(header.Faces,&boxes)!=nil||!validImportRef(boxes){return v2input.ErrInvalid}}
 var local bool
 err=database.TenantTransaction(ctx,q.Catalog.Store.DB,q.Catalog.Scope.Tenant,func(tx pgx.Tx)error{
  if err:=q.Catalog.Store.checkPreparedRun(ctx,tx);err!=nil{return err}
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_import_historical_document_authorized($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,q.Catalog.Scope.Tenant,q.Catalog.Scope.Workflow,string(q.Catalog.Store.Parsed),q.Old.Source.ArtifactID,q.Old.Source.SHA256,kind,ref.ArtifactID,ref.SHA256,imageRef.ArtifactID,imageRef.SHA256,header.Model,header.Backend,boxes.ArtifactID,boxes.SHA256).Scan(&local);err!=nil{return err};return nil
 });if err!=nil{return err}
 if !local{if q.Foreign==nil{return &ImportRecipeUnsupported{Reason:"historical_original_producer_receipt_required"}};if err:=q.Foreign.QualifyForeignHistoricalDocument(ctx,kind,ref,append([]byte(nil),raw...),old,target);err!=nil{return err}}
 return q.Sources.QualifyImportRecipeSource(ctx,q.Catalog.Scope.Entry,old,target)
}
func(q ImportHistoricalNativeAuthority)preparedRef(ctx context.Context,original v2input.Ref,kind string)(v2input.Ref,error){
 if kind=="WorkingImage"&&original==q.Old.Source{return q.Target.Source,nil}
 if kind=="DenseWarpField"{return q.ResolveHistoricalGeometryField(ctx,original)}
 id:=uuid.NewSHA1(q.Catalog.Scope.Workflow,[]byte("interchange:dependency:"+q.Catalog.Scope.Entry.String()+":"+kind+":"+original.ArtifactID.String()))
 binding,err:=canonical.Marshal(q.Target);if err!=nil{return v2input.Ref{},err};var raw []byte
 err=database.TenantTransaction(ctx,q.Catalog.Store.DB,q.Catalog.Scope.Tenant,func(tx pgx.Tx)error{return tx.QueryRow(ctx,`SELECT photo.v30_interchange_prepared_ref($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`,q.Catalog.Scope.Tenant,q.Catalog.Scope.Workflow,string(q.Catalog.Store.Parsed),q.Catalog.Scope.Entry,string(binding),id,kind,original.ArtifactID,original.SHA256).Scan(&raw)})
 if err!=nil{return v2input.Ref{},err};var ref v2input.Ref;if decodeImportDocument(raw,&ref)!=nil||ref.ArtifactID!=id||!validImportRef(ref){return v2input.Ref{},v2input.ErrInvalid};return ref,nil
}
// Replay only the closed mechanical remap over independently qualified original
// bytes. This catches edited landmarks/confidences, replaced pixels and dropped refs.
func(q ImportHistoricalNativeAuthority)QualifyPreparedHistoricalDocument(ctx context.Context,kind string,raw []byte,target v2input.SessionVersionBinding)error{
 if target!=q.Target{return v2input.ErrInvalid};ref,original,err:=q.original(ctx,kind);if err!=nil{return err}
 q.source=ref;if err=q.QualifyArchivedHistoricalDocument(ctx,kind,original,q.Old,q.Target);err!=nil{return err}
 expected,err:=(ImportHistoricalDocumentCodec{Authority:historicalReplayAuthority{}}).RemapImportDocument(ctx,kind,original,q.Old,q.Target,func(ref v2input.Ref,kind string)(v2input.Ref,error){return q.preparedRef(ctx,ref,kind)});if err!=nil{return err}
 if !bytes.Equal(expected,raw){return v2input.ErrInvalid};return ctx.Err()
}
// Private replay port grants no standalone authority; caller already qualified
// the original and every replacement reference through SQL current scope.
type historicalReplayAuthority struct{}
func(historicalReplayAuthority)QualifyArchivedHistoricalDocument(context.Context,string,[]byte,v2input.SessionVersionBinding,v2input.SessionVersionBinding)error{return nil}
func(historicalReplayAuthority)QualifyPreparedHistoricalDocument(context.Context,string,[]byte,v2input.SessionVersionBinding)error{return nil}

// Historical pixels retain their own arrays, while a byte-identical geometry
// field can reuse the exact independently authenticated current-master pin.
// Matching coordinates or SHA alone cannot select an unrelated warp.
func(q ImportHistoricalNativeAuthority)ResolveHistoricalGeometryField(ctx context.Context,ref v2input.Ref)(v2input.Ref,error){
 if q.Masters==nil||q.Sources==nil||!validImportRef(ref){return v2input.Ref{},v2input.ErrInvalid}
 if err:=q.Sources.QualifyImportRecipeSource(ctx,q.Catalog.Scope.Entry,q.Old,q.Target);err!=nil{return v2input.Ref{},err}
 raw,err:=q.Masters.ReadImportMaster(ctx,q.Target.Source,1<<20);if err!=nil{return v2input.Ref{},err};if rawSHA(raw)!=q.Target.Source.SHA256{return v2input.Ref{},v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(raw);if err!=nil{return v2input.Ref{},err}
 var chain struct{Steps []struct{Kind string `json:"kind"`;Field v2input.Ref `json:"inverse_field"`} `json:"steps"`}
 if json.Unmarshal(image.TransformChain,&chain)!=nil{return v2input.Ref{},v2input.ErrInvalid}
 found:=false;for _,step:=range chain.Steps{if step.Kind=="dense_warp"&&step.Field==ref{found=true}}
 if !found{return v2input.Ref{},v2input.ErrInvalid}
 if err=q.Sources.QualifyImportRecipeSource(ctx,q.Catalog.Scope.Entry,q.Old,q.Target);err!=nil{return v2input.Ref{},err}
 return ref,ctx.Err()
}
