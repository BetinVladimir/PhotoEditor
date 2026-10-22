package v2render

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// AuthoredFinalInputEvidence owns the complete scoped typed traversal. It is
// not a substitute for final Params publication or protected task registration.
type AuthoredFinalInputEvidence struct {
 Params CompiledAuthoredFinalParams
 Inputs v2draft.TaskInputEvidence
}

// ReadAuthoredFinalInputEvidence consumes an ACTUALLY PUBLISHED Params ref.
// PhaseObjects must authorize that exact document under the final instruction;
// a future UUID or caller-provided body cannot satisfy this IO boundary.
func ReadAuthoredFinalInputEvidence(ctx context.Context,scheduler *pgxpool.Pool,objects SQLAuthoredPhaseObjects,who auth.Identity,tenant,parent uuid.UUID,ordinal int,paramsRef v2input.Ref)(AuthoredFinalInputEvidence,error){
 empty:=AuthoredFinalInputEvidence{}
 catalog,err:=NewSQLAuthoredFinalCatalog(ctx,scheduler,objects,who,tenant,parent,ordinal);if err!=nil{return empty,err}
 documents,found,err:=ReadAuthoredFinalDocuments(ctx,scheduler,objects,who,tenant,parent,ordinal);if err!=nil{return empty,err};if !found{return empty,v2input.ErrInvalid}
 raw,err:=objects.ReadJSON(ctx,who,tenant,catalog.Project,parent,ordinal,paramsRef);if err!=nil{return empty,err}
 var params AuthoredFinalParams;if decodeAuthoredParams(raw,&params)!=nil{return empty,v2input.ErrInvalid}
 compiled,err:=CompileAuthoredFinalParams(params);if err!=nil||!bytes.Equal(compiled.JSON,raw){return empty,v2input.ErrInvalid}
 continuation,_,_,err:=CompileAuthoredFinalContinuation(documents);if err!=nil{return empty,err}
 continuationRaw,err:=objects.ReadJSON(ctx,who,tenant,catalog.Project,parent,ordinal,params.GeometryContinuation);if err!=nil{return empty,err}
 if !bytes.Equal(continuation,continuationRaw){return empty,v2input.ErrInvalid}
 expected,err:=MaterializeAuthoredFinalParams(documents,params.GeometryContinuation);if err!=nil||!bytes.Equal(expected.JSON,compiled.JSON){return empty,v2input.ErrInvalid}
 roots,err:=AuthoredFinalInputRoots(compiled,paramsRef);if err!=nil{return empty,err}
 // The original full recipe/source binding remains the traversal anchor. Each
 // authored WorkingImage occurrence supplies its own scoped target geometry.
 var base struct{Original v2input.SessionVersionBinding `json:"original_source"`}
 // This is a projection from the already verified immutable base receipt;
 // its complete closed schema is independently parsed by typed traversal.
 if json.Unmarshal([]byte(documents.Inventory.BaseReceiptJCS),&base)!=nil||!authoredBindingValid(base.Original){return empty,v2input.ErrInvalid}
 resolver:=v2draft.ClosureResolver{Catalog:catalog,Objects:objects.Objects}
 // Ordinary render admission retains its original two-root whitelist. This
 // protected continuation walks the original anchor first and the ACTUAL
 // published Params second; typed Params descendants supply its full closure.
 nodes:=[]v2draft.LayerRef{
  {Path:"AuthoredFinal.original_source",Contract:"WorkingImage",Ref:base.Original.Source,CanvasHash:base.Original.CanvasHash},
  {Path:"AuthoredFinal.params",Contract:"Params",Ref:paramsRef},
 }
 inputs,err:=resolver.ResolveBoundProvenanceEvidence(ctx,who,tenant,nodes,base.Original);if err!=nil{return empty,err}
 // A transitive resolver must retain every exact compiler-owned root. An
 // opaque artifact alias alone cannot replace its precise required contract.
 if !authoredFinalCompilerRootsRetained(roots,inputs.Entries){return empty,v2input.ErrInvalid}

 after,present,err:=ReadAuthoredFinalInventory(ctx,scheduler,tenant,parent,ordinal);if err!=nil{return empty,err};if !present{return empty,v2input.ErrInvalid}
 a,err:=canonical.Marshal(documents.Inventory);if err!=nil{return empty,err};b,err:=canonical.Marshal(after);if err!=nil||!bytes.Equal(a,b){return empty,v2input.ErrInvalid}
 return AuthoredFinalInputEvidence{Params:compiled,Inputs:inputs},nil
}

// Exact typed equality is intentional: opaque Artifact provenance cannot satisfy
// a Params/WorkingImage requirement, even if it carries the same object ref.
func authoredFinalCompilerRootsRetained(roots,actual []v2input.Entry)bool{
 for _,required:=range roots{
  found:=false
  for _,entry:=range actual{if entry==required{found=true;break}}
  if !found{return false}
 }
 return true
}
