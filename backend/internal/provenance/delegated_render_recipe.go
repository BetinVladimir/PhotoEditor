package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/v2draft"
)

func ReadDelegatedRenderRecipe(ctx context.Context, documents DelegatedTaskDocuments,
 task CanonicalSigningTask) (v2draft.FrozenRenderRecipe,error) {
 var zero v2draft.FrozenRenderRecipe
 if err:=ctx.Err();err!=nil{return zero,err}
 if documents==nil||task.Operation!="render.compose"||task.Version!="1"||
  normalizeCanonicalSigningTask(&task)!=nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 raw,err:=documents.ReadDelegatedTaskDocument(ctx,owned,owned.Params,1<<20)
 if err!=nil{return zero,err}
 if !pinned(raw,owned.Params,1<<20){return zero,ErrInvalid}
 body:=append([]byte(nil),raw...)
 recipe,err:=v2draft.DecodeFrozenRenderRecipe(body)
 if err!=nil{return zero,ErrInvalid}
 current,err:=documents.ReadDelegatedTaskDocument(ctx,owned,owned.Params,1<<20)
 if err!=nil{return zero,err}
 if !bytes.Equal(current,body){return zero,ErrInvalid}
 return recipe,ctx.Err()
}
