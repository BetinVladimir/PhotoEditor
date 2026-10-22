package v2render

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type AuthoredPublishedPhase struct {
 Ordinal int `json:"ordinal"`
 Kind string `json:"kind"`
 Instruction uuid.UUID `json:"instruction_id"`
 Receipt v2input.Ref `json:"receipt"`
 ReceiptJCS string `json:"receipt_jcs"`
}
type AuthoredFinalInventory struct {
 Context AuthoredInstructionSource `json:"context"`
 BaseReceipt v2input.Ref `json:"base_receipt"`
 BaseReceiptJCS string `json:"base_receipt_jcs"`
 Phases []AuthoredPublishedPhase `json:"phases"`
}

// ReadAuthoredFinalInventory selects already-published real receipts in SQL.
// It cannot promote arbitrary ready artifacts into canonical inputs. All
// returned JCS bodies are independently hashed and recompiled before use; the
// physical input resolver must still verify actual versioned object bytes and
// expand typed descendants, followed by this same current authority recheck.
func ReadAuthoredFinalInventory(ctx context.Context,scheduler *pgxpool.Pool,tenant,parent uuid.UUID,ordinal int)(AuthoredFinalInventory,bool,error){
 empty:=AuthoredFinalInventory{}
 fail:=func()(AuthoredFinalInventory,bool,error){return empty,false,v2input.ErrInvalid}
 if scheduler==nil||tenant==uuid.Nil||parent==uuid.Nil||ordinal<2||ordinal>=5000{return fail()}
 var role string;if err:=scheduler.QueryRow(ctx,`SELECT current_user`).Scan(&role);err!=nil{return empty,false,err};if role!="photo_scheduler"{return fail()}
 var raw []byte;if err:=scheduler.QueryRow(ctx,`SELECT photo.authored_render_final_input_context($1,$2,$3)`,tenant,parent,ordinal).Scan(&raw);err!=nil{return empty,false,err}
 if len(raw)==0||bytes.Equal(raw,[]byte("null")){return empty,false,nil}
 if len(raw)>64<<20{return fail()}
 var out AuthoredFinalInventory;if decodeAuthoredParams(raw,&out)!=nil{return fail()}
 source:=out.Context
 if source.Tenant!=tenant||source.Parent!=parent||source.Instruction.Ordinal!=ordinal||source.Instruction.Kind!="finalize"||len(out.Phases)!=ordinal-1{return fail()}
 fresh,found,err:=ReadAuthoredInstructionSource(ctx,scheduler,tenant,parent,ordinal)
 if err!=nil{return empty,false,err};if !found{return empty,false,nil}
 a,err:=canonical.Marshal(source);if err!=nil{return fail()};b,err:=canonical.Marshal(fresh);if err!=nil||!bytes.Equal(a,b){return fail()}
 verify:=func(ref v2input.Ref,body string)bool{
  if ref.ArtifactID==uuid.Nil||len(body)<2||len(body)>8<<20{return false}
  digest:=sha256.Sum256([]byte(body));wire,e:=canonical.Bytes([]byte(body))
  return e==nil&&bytes.Equal(wire,[]byte(body))&&hex.EncodeToString(digest[:])==ref.SHA256
 }
 if !verify(out.BaseReceipt,out.BaseReceiptJCS){return fail()}
 var base struct{
  Tenant uuid.UUID `json:"tenant_id"`;Parent uuid.UUID `json:"parent_run_id"`
  Original v2input.Ref `json:"original_recipe"`;Processed v2input.SessionVersionBinding `json:"processed_source"`
 }
 if json.Unmarshal([]byte(out.BaseReceiptJCS),&base)!=nil||base.Tenant!=tenant||base.Parent!=parent||base.Original!=source.OriginalRecipe||!authoredBindingValid(base.Processed){return fail()}
 previous:=out.BaseReceipt.SHA256;current:=base.Processed;seen:=map[uuid.UUID]bool{out.BaseReceipt.ArtifactID:true}
 for i,item:=range out.Phases{
  if item.Ordinal!=i+1||item.Instruction==uuid.Nil||seen[item.Receipt.ArtifactID]||!verify(item.Receipt,item.ReceiptJCS){return fail()}
  var phase AuthoredPhaseReceipt;if decodeAuthoredParams([]byte(item.ReceiptJCS),&phase)!=nil{return fail()}
  if phase.Tenant!=tenant||phase.Parent!=parent||phase.Ordinal!=item.Ordinal||phase.Kind!=item.Kind||phase.Instruction!=item.Instruction||phase.InputSource!=current||phase.PredecessorSHA!=previous||phase.ProgramSHA!=source.ProgramSHA||phase.OriginalRecipe!=source.OriginalRecipe{return fail()}
  if phase.Kind!="segment"&&phase.Kind!="geometry"{return fail()}
  bound:=source;bound.Source=current;bound.PredecessorSHA=previous
  bound.Instruction.Ordinal=phase.Ordinal;bound.Instruction.Identity=phase.Instruction;bound.Instruction.Kind=phase.Kind
  body,sha,err:=CompileAuthoredPhaseReceipt(bound,phase.AuthoredPhaseTask,phase.OutputSource,phase.Result,phase.Report,phase.Geometry,phase.PhysicalClosure)
  if err!=nil||sha!=item.Receipt.SHA256||!bytes.Equal(body,[]byte(item.ReceiptJCS)){return fail()}
  current=phase.OutputSource;previous=item.Receipt.SHA256;seen[item.Receipt.ArtifactID]=true
 }
 if current!=source.Source||previous!=source.PredecessorSHA{return fail()}
 if err=ctx.Err();err!=nil{return empty,false,err}
 return out,true,nil
}
