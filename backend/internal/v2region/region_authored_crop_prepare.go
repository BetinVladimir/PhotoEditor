package v2region

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type authoredCropCatalog struct{evidence SQLAuthoredRenderEvidence;record AuthoredRegionRecord}
func(c authoredCropCatalog)Lookup(ctx context.Context,who auth.Identity,tenant uuid.UUID,node v2draft.LayerRef)(objectstore.Object,error){
 if tenant!=c.record.Intent.Tenant||who.UserID!=c.record.Intent.Actor||who.SessionID!=c.record.Intent.Session{return objectstore.Object{},ErrRegionInvalid}
 // The existing typed walker validates occurrences, schema, expected canvas,
 // parent references and content. SQL independently checks membership in the
 // one canonical render's registered inputs or adopted output inventory.
 return c.evidence.metadata(ctx,c.record,node.Ref)
}

// Prepare resolves the entire authorized closure before uploading Params.
// Physically written Params use the existing version-scoped immutable draft
// protocol, exactly as scalar region Params do; no READY rows are constructed.
func(s SQLAuthoredRenderEvidence)PrepareCrop(ctx context.Context,r AuthoredRegionRecord,publisher RegionParamsPublisher,profile []byte)(FrozenAuthoredCropNode,error){
 var empty FrozenAuthoredCropNode
 if publisher==nil{return empty,ErrRegionUnavailable}
 plan,err:=s.Load(ctx,r);if err!=nil{return empty,err}
 who:=auth.Identity{UserID:r.Intent.Actor,SessionID:r.Intent.Session}
 resolver:=v2draft.ClosureResolver{Catalog:authoredCropCatalog{s,r},Objects:s.Objects}
 entries,err:=resolver.ResolveAuthoredRegionInputs(ctx,who,r.Intent.Tenant,r.Intent.Binding,r.Intent.Recipe,plan.params.RenderResult)
 if err!=nil{return empty,err}
 // Estimate from actual catalog byte sizes, including Params, without trusting
 // a public caller's byte count or retaining all binary inputs in API memory.
 inputBytes:=int64(len(plan.body))
 for _,entry:=range entries{metadata,e:=s.metadata(ctx,r,entry.Ref);if e!=nil{return empty,e};if metadata.Bytes>9007199254740991-inputBytes{return empty,ErrRegionInvalid};inputBytes+=metadata.Bytes}
 resources,err:=CompileAuthoredCropResources(plan.rendered.Canvas.Width,plan.rendered.Canvas.Height,inputBytes,plan.params.MaxEdge,plan.rendered.Alpha!=nil)
 if err!=nil{return empty,err}
 baseProfile,err:=canonical.Bytes(profile);if err!=nil{return empty,ErrRegionInvalid}
 var fields map[string]json.RawMessage
 if json.Unmarshal(baseProfile,&fields)!=nil||fields==nil{return empty,ErrRegionInvalid}
 for _,key:=range []string{"contracts_sha","v2_input_manifest","v2_entry_count","v2_entries_sha","authored_region_intent_sha256","authored_region_input_bytes","resource_estimate"}{if _,ok:=fields[key];ok{return empty,ErrRegionInvalid}}
 fields["authored_region_intent_sha256"],err=json.Marshal(plan.params.IntentSHA);if err!=nil{return empty,err}
 fields["authored_region_input_bytes"],err=json.Marshal(inputBytes);if err!=nil{return empty,err}
 fields["resource_estimate"],err=canonical.Marshal(resources);if err!=nil{return empty,err}
 profile,err=canonical.Marshal(fields);if err!=nil{return empty,err}
 workflow,_:=v2input.WorkflowID(r.CropRun);task,_:=v2input.TaskID(workflow,"region.authored_crop",0)
 placeholder:=v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("authored-region.crop.params.preflight:1")),SHA256:plan.sha}
 if _,err=plan.CompileNode(task,placeholder,entries,profile);err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 params,err:=publisher.Publish(ctx,who,r.Intent.Tenant,r.Intent.Binding.AssetVersionID,uuid.NewSHA1(task,[]byte("authored-region.crop.params:1")),plan.JSON())
 if err!=nil{return empty,err};if params.ArtifactID==uuid.Nil||params.SHA256!=plan.sha{return empty,ErrRegionInvalid}
 // Re-reading the actual task/adoption/root/typed documents after publication
 // prevents an uploaded object from turning withdrawn authority into admission.
 latest,err:=s.Load(ctx,r);if err!=nil{return empty,err};if latest.sha!=plan.sha{return empty,v2input.ErrConflict}
 return latest.CompileNode(task,params,entries,profile)
}
