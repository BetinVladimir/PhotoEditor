package v2optical

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type negativeAffectedSourceCatalog interface{lookup(context.Context,PreparedNegativeReplacementStep)([]byte,error)}
type sqlNegativeAffectedSourceCatalog struct{db NegativeTransactions}
func(c sqlNegativeAffectedSourceCatalog)lookup(ctx context.Context,s PreparedNegativeReplacementStep)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err};if c.db==nil{return nil,v2input.ErrInvalid}
 l:=s.intent.locator;who:=s.intent.who
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=c.db.Begin(ctx);if err!=nil{return nil,err};if tx==nil{return nil,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string;if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return nil,err};if role!="photo_scheduler"{return nil,v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",l.Tenant.String());err!=nil{return nil,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_affected_source_inputs($1,$2,$3,$4,$5,$6,$7)",l.Tenant,l.Project,who.UserID,who.SessionID,l.Review,l.Hash,l.Task).Scan(&raw);err!=nil{return nil,err}
 if _,err=decodeNegativeAffectedSources(raw);err!=nil{return nil,err}
 if err=tx.Commit(ctx);err!=nil{return nil,err};return raw,nil
}

// VerifiedNegativeAffectedSources owns persisted changed-input mapping and exact
// frozen step. Repeated reads prove a stable lookup, not retained SQL authority.
type VerifiedNegativeAffectedSources struct{step PreparedNegativeReplacementStep;raw string;sources []matchedNegativeAffectedSource}
func LoadNegativeAffectedSources(ctx context.Context,db NegativeTransactions,s PreparedNegativeReplacementStep)(VerifiedNegativeAffectedSources,error){
 return loadNegativeAffectedSources(ctx,sqlNegativeAffectedSourceCatalog{db},s)
}
func loadNegativeAffectedSources(ctx context.Context,catalog negativeAffectedSourceCatalog,s PreparedNegativeReplacementStep)(VerifiedNegativeAffectedSources,error){
 var zero VerifiedNegativeAffectedSources
 if err:=ctx.Err();err!=nil{return zero,err};if catalog==nil{return zero,v2input.ErrInvalid}
 l:=s.intent.locator
 packet,err:=canonical.Marshal(l);if err!=nil{return zero,err}
 if _,err:=decodeNegativeFrozenIntent(packet,l.Params.Bucket,l.Tenant,l.Project,l.Review,l.Task,l.Hash);err!=nil{return zero,err}
 sum:=sha256.Sum256([]byte(s.intent.raw))
 if s.intent.who.UserID==uuid.Nil||s.intent.who.SessionID==uuid.Nil||s.node.OriginalTask!=l.Task||s.node.OriginalRun!=l.Run||int64(len(s.intent.raw))!=l.Params.Bytes||hex.EncodeToString(sum[:])!=l.Params.SHA256{return zero,v2input.ErrInvalid}
 s.node.Parents=append([]NegativeReplacementParent(nil),s.node.Parents...)
 lookup:=func()([]byte,error){owned:=s;owned.node.Parents=append([]NegativeReplacementParent(nil),s.node.Parents...);return catalog.lookup(ctx,owned)}
 before,err:=lookup();if err!=nil{return zero,err}
 matched,err:=matchNegativeAffectedSources(ctx,l.Operation,[]byte(s.intent.raw),s.node.Parents,before);if err!=nil{return zero,err}
 normal,err:=canonical.Bytes(before);if err!=nil{return zero,err}
 after,err:=lookup();if err!=nil{return zero,err}
 if _,err=decodeNegativeAffectedSources(after);err!=nil{return zero,err}
 repeated,err:=canonical.Bytes(after);if err!=nil||!bytes.Equal(normal,repeated){return zero,v2input.ErrExpired}
 return VerifiedNegativeAffectedSources{step:s,raw:string(normal),sources:append(make([]matchedNegativeAffectedSource,0,len(matched)),matched...)},ctx.Err()
}
