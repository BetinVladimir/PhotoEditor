package v2optical

import (
 "context"
 "photoeditor/backend/internal/v2input"
)

// LoadMappedReadySources loads exact affected-source mappings and all unchanged
// original-version documents/arrays for the complete ready wave. Rebuilt and
// retained parents must already be physically verified adopted results. No
// cancellation/publication or admission authority is produced by this loader.
func(p PreparedNegativeReplacementInputs)LoadMappedReadySources(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,rebuilt,retained []VerifiedAdoptedParentArrays)([]VerifiedNegativeAffectedSources,[]VerifiedNegativeUnchangedArrays,error){
 if err:=ctx.Err();err!=nil{return nil,nil,err}
 if db==nil||objects==nil||len(p.bucket)<3||len(p.bucket)>63{return nil,nil,v2input.ErrInvalid}
 parents,err:=p.ReadyParentSources(ctx,rebuilt,retained);if err!=nil{return nil,nil,err}
 mappings:=make([]VerifiedNegativeAffectedSources,0,len(parents))
 for _,parent:=range parents{
  if err:=ctx.Err();err!=nil{return nil,nil,err}
  mapping,err:=LoadNegativeAffectedSources(ctx,db,parent.step);if err!=nil{return nil,nil,err}
  mappings=append(mappings,mapping)
 }
 ready,err:=p.prepareMappedReadySources(ctx,rebuilt,retained,mappings);if err!=nil{return nil,nil,err}
 unchanged:=make([]VerifiedNegativeUnchangedArrays,0)
 for _,step:=range ready{
  required,err:=step.requiredUnchangedBindings(ctx);if err!=nil{return nil,nil,err}
  for _,binding:=range required{
   if err:=ctx.Err();err!=nil{return nil,nil,err}
   source,err:=LoadNegativeUnchangedSource(ctx,db,objects,p.bucket,step.parents.step,binding);if err!=nil{return nil,nil,err}
   arrays,err:=LoadNegativeUnchangedArrays(ctx,db,objects,p.bucket,p.who,source);if err!=nil{return nil,nil,err}
   unchanged=append(unchanged,arrays)
  }
 }
 if _,err=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);err!=nil{return nil,nil,err}
 return mappings,unchanged,ctx.Err()
}
