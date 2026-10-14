package v2optical

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type negativeFrozenIntentLocator struct {
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 Hash string `json:"decision_hash"`
 Task uuid.UUID `json:"original_task_id"`
 Run uuid.UUID `json:"original_run_id"`
 Operation string `json:"operation"`
 Params negativeSourceLocator `json:"params"`
}
func decodeNegativeFrozenIntent(raw []byte,bucket string,tenant,project,reviewID,task uuid.UUID,hash string)(negativeFrozenIntentLocator,error){
 var zero negativeFrozenIntentLocator
 if len(raw)<2||len(raw)>32768 {return zero,v2input.ErrExpired}
 normal,err:=canonical.Bytes(raw);if err!=nil {return zero,v2input.ErrInvalid}
 var out negativeFrozenIntentLocator
 if json.Unmarshal(normal,&out)!=nil {return zero,v2input.ErrInvalid};exact,err:=canonical.Marshal(out)
 if err!=nil||!bytes.Equal(exact,normal)||out.SchemaVersion!=1||out.Tenant!=tenant||out.Project!=project||out.Review!=reviewID||out.Task!=task||out.Hash!=hash||out.Run==uuid.Nil {return zero,v2input.ErrInvalid}
 switch out.Operation {case "optical.negative","optical.perspective","optical.panorama","optical.focus_stack":default:return zero,v2input.ErrInvalid}
 p:=out.Params
 if !validPerspectiveRef(v2input.Ref{ArtifactID:p.ArtifactID,SHA256:p.SHA256})||p.Bucket!=bucket||p.Key==""||len(p.Key)>1024||p.Version==""||p.Version=="null"||len(p.Version)>1024||p.Bytes<2||p.Bytes>65536||p.Media!="application/json" {return zero,v2input.ErrInvalid}
 return out,nil
}
type negativeFrozenIntentCatalog interface {lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,string)(negativeFrozenIntentLocator,error)}
type SQLNegativeFrozenIntentCatalog struct {DB NegativeTransactions;Bucket string}
func(c SQLNegativeFrozenIntentCatalog)lookup(ctx context.Context,who auth.Identity,tenant,project,reviewID,task uuid.UUID,hash string)(negativeFrozenIntentLocator,error){
 var zero negativeFrozenIntentLocator
 if err:=ctx.Err();err!=nil {return zero,err}
 if c.DB==nil||c.Bucket==""||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||project==uuid.Nil||reviewID==uuid.Nil||task==uuid.Nil {return zero,v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(hash);err!=nil {return zero,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel();tx,err:=c.DB.Begin(ctx);if err!=nil {return zero,err};if tx==nil {return zero,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string;if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return zero,err};if role!="photo_scheduler" {return zero,v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil {return zero,err}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_frozen_intent($1,$2,$3,$4,$5,$6,$7)",tenant,project,who.UserID,who.SessionID,reviewID,hash,task).Scan(&raw);err!=nil {return zero,err}
 locator,err:=decodeNegativeFrozenIntent(raw,c.Bucket,tenant,project,reviewID,task,hash);if err!=nil {return zero,err}
 if err:=tx.Commit(ctx);err!=nil {return zero,err};return locator,nil
}

// VerifiedOpticalFrozenIntent is issued only from original immutable Params
// bytes and repeated current invalidation/catalogue authority. It is not an
// execution profile or replacement-admission acknowledgement.
type VerifiedOpticalFrozenIntent struct {locator negativeFrozenIntentLocator;raw string;who auth.Identity}
func LoadOpticalFrozenIntent(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,who auth.Identity,tenant,project,reviewID,task uuid.UUID,hash string)(VerifiedOpticalFrozenIntent,error){
 return loadOpticalFrozenIntent(ctx,SQLNegativeFrozenIntentCatalog{DB:db,Bucket:bucket},objects,who,tenant,project,reviewID,task,hash)
}
func loadOpticalFrozenIntent(ctx context.Context,catalog negativeFrozenIntentCatalog,objects v2input.SessionAnalysisObjectReader,who auth.Identity,tenant,project,reviewID,task uuid.UUID,hash string)(VerifiedOpticalFrozenIntent,error){
 var zero VerifiedOpticalFrozenIntent
 if err:=ctx.Err();err!=nil {return zero,err};if catalog==nil||objects==nil {return zero,v2input.ErrInvalid}
 locator,err:=catalog.lookup(ctx,who,tenant,project,reviewID,task,hash);if err!=nil {return zero,err};p:=locator.Params
 stream,err:=objects.Open(ctx,objectstore.Object{Key:p.Key,VersionID:p.Version,SHA256:p.SHA256,Bytes:p.Bytes,ContentType:p.Media})
 if err!=nil {if stream!=nil {_=stream.Close()};return zero,err};if stream==nil {return zero,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(stream,p.Bytes+1));closeErr:=stream.Close()
 if readErr!=nil {return zero,readErr};if closeErr!=nil {return zero,closeErr};if err:=ctx.Err();err!=nil {return zero,err}
 sum:=sha256.Sum256(raw);if int64(len(raw))!=p.Bytes||hex.EncodeToString(sum[:])!=p.SHA256 {return zero,v2input.ErrInvalid}
 switch locator.Operation {
 case "optical.negative":_,err=ParseNegativeParams(raw)
 case "optical.perspective":_,err=ParsePerspectiveParams(raw)
 case "optical.panorama":_,err=ParsePanoramaParams(raw)
 case "optical.focus_stack":_,err=ParseFocusStackParams(raw)
 default:err=v2input.ErrInvalid
 };if err!=nil {return zero,err}
 current,err:=catalog.lookup(ctx,who,tenant,project,reviewID,task,hash);if err!=nil {return zero,err};if current!=locator {return zero,v2input.ErrExpired}
 return VerifiedOpticalFrozenIntent{locator:locator,raw:string(raw),who:who},nil
}
func(v VerifiedOpticalFrozenIntent)Rebind(ctx context.Context,who auth.Identity,tenant,project,reviewID,task uuid.UUID,hash string,bindings []OpticalReplacementBinding,lines *v2input.Ref)(ReboundOpticalIntent,error){
 s:=v.locator
 if s.SchemaVersion!=1||s.Tenant!=tenant||s.Project!=project||s.Review!=reviewID||s.Task!=task||s.Hash!=hash||v.who.UserID!=who.UserID||v.who.SessionID!=who.SessionID||who.UserID==uuid.Nil||who.SessionID==uuid.Nil {return ReboundOpticalIntent{},v2input.ErrInvalid}
 return RebindOpticalIntent(ctx,s.Operation,[]byte(v.raw),bindings,lines)
}
