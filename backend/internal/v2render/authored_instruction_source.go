package v2render

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/domain"
)

// AuthoredInstructionSource is a fresh SQL observation, not a transferable
// admission capability. Every publisher/stager must recheck the locked ledger.
type AuthoredInstructionSource struct {
 Tenant uuid.UUID `json:"tenant"`
 Parent uuid.UUID `json:"parent"`
 Project uuid.UUID `json:"project"`
 Workflow uuid.UUID `json:"workflow"`
 Actor uuid.UUID `json:"actor"`
 Session uuid.UUID `json:"session"`
 ProgramSHA string `json:"program_sha"`
 Instruction AuthoredGeometryInstruction `json:"instruction"`
 Source v2input.SessionVersionBinding `json:"source"`
 PredecessorSHA string `json:"predecessor_sha"`
 PredecessorJCS string `json:"predecessor_jcs"`
 OriginalRecipe v2input.Ref `json:"original_recipe"`
 RecipeJCS string `json:"recipe_jcs"`
 LayerStackJCS string `json:"layer_stack_jcs"`
}

// ReadAuthoredInstructionSource takes no replacement source/receipt/body from
// its caller. The actual canonical predecessor is selected by the immutable
// parent instruction chain under SQL locks; physical readers still repeat the
// returned object hash/version and their stage-specific original attribution.
func ReadAuthoredInstructionSource(ctx context.Context,scheduler *pgxpool.Pool,tenant,parent uuid.UUID,ordinal int)(AuthoredInstructionSource,bool,error){
 fail:=func()(AuthoredInstructionSource,bool,error){return AuthoredInstructionSource{},false,v2input.ErrInvalid}
 if scheduler==nil||tenant==uuid.Nil||parent==uuid.Nil||ordinal<1||ordinal>=5000{return fail()}
 var role string;if err:=scheduler.QueryRow(ctx,`SELECT current_user`).Scan(&role);err!=nil{return AuthoredInstructionSource{},false,err};if role!="photo_scheduler"{return fail()}
 var raw []byte;if err:=scheduler.QueryRow(ctx,`SELECT photo.authored_render_instruction_context($1,$2,$3)`,tenant,parent,ordinal).Scan(&raw);err!=nil{return AuthoredInstructionSource{},false,err}
 if len(raw)==0||bytes.Equal(raw,[]byte("null")){return AuthoredInstructionSource{},false,nil}
 if len(raw)>4<<20{return fail()}
 var out AuthoredInstructionSource;decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields();if decoder.Decode(&out)!=nil{return fail()}
 var tail any;if decoder.Decode(&tail)!=io.EOF{return fail()}
 if out.Tenant!=tenant||out.Parent!=parent||out.Project==uuid.Nil||out.Actor==uuid.Nil||out.Session==uuid.Nil||out.Workflow!=uuid.NewSHA1(parent,[]byte("workflow"))||out.Instruction.Ordinal!=ordinal||out.Instruction.Predecessor==nil||!authoredBindingValid(out.Source){return fail()}
 expected,err:=v2input.TaskID(out.Workflow,"render.authored_"+out.Instruction.Kind,ordinal);if err!=nil||expected!=out.Instruction.Identity{return fail()}
 switch out.Instruction.Kind{case "geometry":if out.Instruction.GeometryLayer==nil||len(out.Instruction.Layers)!=0{return fail()};case "segment":if out.Instruction.GeometryLayer!=nil||len(out.Instruction.Layers)!=1{return fail()};case "finalize":if out.Instruction.GeometryLayer!=nil||len(out.Instruction.Layers)!=0{return fail()};default:return fail()}
 for _,item:=range []struct{Body,SHA string}{{out.PredecessorJCS,out.PredecessorSHA},{out.RecipeJCS,out.OriginalRecipe.SHA256}}{
  if len(item.Body)<2||len(item.Body)>1048576{return fail()};sum:=sha256.Sum256([]byte(item.Body));if hex.EncodeToString(sum[:])!=item.SHA{return fail()}
 }
 var recipe struct{Recipe struct{Layers v2input.Ref `json:"layer_stack"`;Source v2input.SessionVersionBinding `json:"source"`} `json:"recipe"`}
 if json.Unmarshal([]byte(out.RecipeJCS),&recipe)!=nil||recipe.Recipe.Layers.ArtifactID==uuid.Nil||len(out.LayerStackJCS)<2||len(out.LayerStackJCS)>1048576{return fail()}
 layerHash:=sha256.Sum256([]byte(out.LayerStackJCS));if hex.EncodeToString(layerHash[:])!=recipe.Recipe.Layers.SHA256{return fail()}
 var layers struct{Source v2input.SessionVersionBinding `json:"source"`;Layers []json.RawMessage `json:"layers"`}
 if json.Unmarshal([]byte(out.LayerStackJCS),&layers)!=nil||len(layers.Layers)==0||len(layers.Layers)>5000||layers.Source!=recipe.Recipe.Source{return fail()}
 for _,value:=range []string{out.ProgramSHA,out.PredecessorSHA,out.OriginalRecipe.SHA256}{if _,err=domain.ParseDigest(value);err!=nil{return fail()}}
 if out.OriginalRecipe.ArtifactID==uuid.Nil||*out.Instruction.Predecessor==uuid.Nil{return fail()}
 if err=ctx.Err();err!=nil{return AuthoredInstructionSource{},false,err}
 return out,true,nil
}
