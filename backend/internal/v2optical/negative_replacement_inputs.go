package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "sort"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// PreparedNegativeReplacementInputs owns the complete base-route source and
// frozen intent package. This is an input gate, not executable graph admission.
type PreparedNegativeReplacementInputs struct {
 bucket string
 who auth.Identity
 tenant,project,review uuid.UUID
 hash,planJCS,inventoryJCS,topologyJCS,fingerprint string
 physical VerifiedNegativeSelectedArrays
 intents []VerifiedOpticalFrozenIntent
}

func PrepareNegativeReplacementInputs(ctx context.Context,bucket string,who auth.Identity,tenant,project,reviewID uuid.UUID,hash string,plan NegativeInvalidationPlan,inventory []NegativeDependencyTask,physical VerifiedNegativeSelectedArrays,intents []VerifiedOpticalFrozenIntent)(PreparedNegativeReplacementInputs,error){
 var zero PreparedNegativeReplacementInputs
 if err:=ctx.Err();err!=nil {return zero,err}
 selection,pins,err:=selectedNegativeProofPacket(bucket,who,tenant,project,reviewID,hash,physical);if err!=nil {return zero,err}
 topology,err:=physical.CompileTopology(ctx,tenant,project,reviewID,hash,plan,inventory,4096,65536);if err!=nil {return zero,err}
 if len(intents)!=len(topology.Nodes) {return zero,v2input.ErrInvalid}
 byTask:=make(map[uuid.UUID]VerifiedOpticalFrozenIntent,len(intents))
 for _,intent:=range intents {
  s:=intent.locator
  if _,duplicate:=byTask[s.Task];duplicate {return zero,v2input.ErrInvalid}
  if intent.who.UserID!=who.UserID||intent.who.SessionID!=who.SessionID||s.Tenant!=tenant||s.Project!=project||s.Review!=reviewID||s.Hash!=hash {return zero,v2input.ErrInvalid}
  raw,err:=canonical.Marshal(s);if err!=nil {return zero,err}
  if _,err:=decodeNegativeFrozenIntent(raw,bucket,tenant,project,reviewID,s.Task,hash);err!=nil {return zero,err}
  sum:=sha256.Sum256([]byte(intent.raw))
  if int64(len(intent.raw))!=s.Params.Bytes||hex.EncodeToString(sum[:])!=s.Params.SHA256 {return zero,v2input.ErrInvalid}
  switch s.Operation {
  case "optical.negative":_,err=ParseNegativeParams([]byte(intent.raw))
  case "optical.perspective":_,err=ParsePerspectiveParams([]byte(intent.raw))
  case "optical.panorama":_,err=ParsePanoramaParams([]byte(intent.raw))
  case "optical.focus_stack":_,err=ParseFocusStackParams([]byte(intent.raw))
  default:err=v2input.ErrInvalid
  };if err!=nil {return zero,err};byTask[s.Task]=intent
 }
 owned:=make([]VerifiedOpticalFrozenIntent,0,len(intents))
 for _,node:=range topology.Nodes {intent,exists:=byTask[node.OriginalTask];if !exists||intent.locator.Run!=node.OriginalRun {return zero,v2input.ErrInvalid};owned=append(owned,intent)}
 sort.Slice(owned,func(i,j int)bool{return owned[i].locator.Task.String()<owned[j].locator.Task.String()})
 planRaw,err:=canonical.Marshal(plan);if err!=nil {return zero,err};inventoryRaw,err:=canonical.Marshal(inventory);if err!=nil {return zero,err};topologyRaw,err:=canonical.Marshal(topology);if err!=nil {return zero,err}
 type intentPin struct{Locator negativeFrozenIntentLocator `json:"locator"`;Original string `json:"original"`}
 frozen:=make([]intentPin,0,len(owned));for _,v:=range owned {frozen=append(frozen,intentPin{Locator:v.locator,Original:v.raw})}
 fingerprint,err:=canonical.Hash(struct{
  Bucket string `json:"bucket"`;Actor uuid.UUID `json:"actor"`;Session uuid.UUID `json:"session"`
  Plan string `json:"plan"`;Inventory string `json:"inventory"`;Topology string `json:"topology"`
  Selection string `json:"selection"`;Pins string `json:"pins"`;Frozen []intentPin `json:"frozen"`
 }{bucket,who.UserID,who.SessionID,string(planRaw),string(inventoryRaw),string(topologyRaw),string(selection),string(pins),frozen});if err!=nil {return zero,err}
 // Source JSON and locator pins must remain stable across arbitrary callbacks.
 locator:=*physical.selection.source.Source;physical.selection.source.Source=&locator
 physical.specs=append([]NegativeSourceArraySpec(nil),physical.specs...);physical.objects=append([]objectstore.Object(nil),physical.objects...)
 return PreparedNegativeReplacementInputs{bucket:bucket,who:who,tenant:tenant,project:project,review:reviewID,hash:hash,planJCS:string(planRaw),inventoryJCS:string(inventoryRaw),topologyJCS:string(topologyRaw),fingerprint:fingerprint,physical:physical,intents:owned},nil
}
func(p PreparedNegativeReplacementInputs)Topology()(NegativeReplacementTopology,error){
 var out NegativeReplacementTopology
 if p.fingerprint==""||json.Unmarshal([]byte(p.topologyJCS),&out)!=nil {return out,v2input.ErrInvalid};return out,nil
}

// Lock first recompiles the complete private packet, then checks scope/settlement,
// source and every frozen intent in one caller-owned transaction. No object IO,
// commit, queueing or replacement receipt is emitted. Roll back on every error.
func(p PreparedNegativeReplacementInputs)Lock(ctx context.Context,tx pgx.Tx)error{
 if err:=ctx.Err();err!=nil {return err};if tx==nil||p.fingerprint=="" {return v2input.ErrInvalid}
 _,plan,_,err:=p.checked(ctx);if err!=nil {return err}
 if err:=LockNegativeReplacementScope(ctx,tx,p.who,p.tenant,p.project,p.review,p.hash,plan);err!=nil {return err}
 if err:=LockNegativeSelectedArrays(ctx,tx,p.bucket,p.who,p.tenant,p.project,p.review,p.hash,p.physical);err!=nil {return err}
 for _,intent:=range p.intents {if err:=LockOpticalFrozenIntent(ctx,tx,p.who,p.tenant,p.project,p.review,intent.locator.Task,p.hash,intent);err!=nil {return err}}
 return ctx.Err()
}
