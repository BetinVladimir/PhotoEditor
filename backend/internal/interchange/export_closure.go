package interchange

import (
 "bytes"
 "context"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type ExportClosureNode = ImportClosureNode
type ExportClosureNodeResolver interface{ResolveExportNode(context.Context,v2input.Entry)(ExportClosureNode,error)}
type ExportGraphResolverFactory interface{BindExportGraph(context.Context,CompiledExportPlan)(ExportClosureNodeResolver,error)}
type ExportInputInventory struct{task uuid.UUID;entries []v2input.Entry;digest string;backgrounds []bundleBackground;backgroundDigest string}
func(i ExportInputInventory)Entries()[]v2input.Entry{return append([]v2input.Entry(nil),i.entries...)}

// Resolver must independently authorize/qualify every typed node's physical
// bytes and source provenance. This walk additionally prevents omitted refs,
// cycles and inconsistent identities before transport publication.
func ResolveExportInputInventory(ctx context.Context,compiled CompiledExportPlan,resolver ExportClosureNodeResolver)(ExportInputInventory,error){
 if err:=ctx.Err();err!=nil{return ExportInputInventory{},err}
 if factory,ok:=resolver.(ExportGraphResolverFactory);ok{
  bound,err:=factory.BindExportGraph(ctx,compiled);if err!=nil{return ExportInputInventory{},err};resolver=bound
 }
 return resolveExportInputInventory(ctx,compiled,resolver,false)
}

// Discovery validates JSON closure only. Its result is never returned as an
// admitted inventory: binary authorization and physical qualification follow
// in the full ResolveExportInputInventory pass after geometry installation.
func DiscoverExportGeometryParents(ctx context.Context,compiled CompiledExportPlan,jsonResolver ExportClosureNodeResolver)([]v2input.Entry,error){
 parents,_,err:=discoverExportGeometryAndDefects(ctx,compiled,jsonResolver)
 return parents,err
}
func discoverExportGeometryAndDefects(ctx context.Context,compiled CompiledExportPlan,jsonResolver ExportClosureNodeResolver)([]v2input.Entry,[]v2input.Entry,error){
 parents,defects,_,err:=discoverExportGeometryDefectsAndLUTs(ctx,compiled,jsonResolver)
 return parents,defects,err
}
func discoverExportGeometryDefectsAndLUTs(ctx context.Context,compiled CompiledExportPlan,jsonResolver ExportClosureNodeResolver)([]v2input.Entry,[]v2input.Entry,[]v2input.Entry,error){
 parents,defects,luts,_,_,err:=discoverExportNativeParents(ctx,compiled,jsonResolver)
 return parents,defects,luts,err
}
func discoverExportNativeParents(ctx context.Context,compiled CompiledExportPlan,jsonResolver ExportClosureNodeResolver)([]v2input.Entry,[]v2input.Entry,[]v2input.Entry,[]v2input.Entry,[]v2input.Entry,error){
 inventory,err:=resolveExportInputInventory(ctx,compiled,jsonResolver,true);if err!=nil{return nil,nil,nil,nil,nil,err}
 parents,defects,resolved,inline:=[]v2input.Entry{},[]v2input.Entry{},[]v2input.Entry{},[]v2input.Entry{}
 reports:=[]v2input.Entry{}
 hasLUT:=false
 for _,entry:=range inventory.entries{
  if entry.Contract=="WorkingImage"||entry.Contract=="MaskRef"{parents=append(parents,entry)}
  if entry.Contract=="DefectProposeReport"{reports=append(reports,entry);inline=append(inline,entry)}
  if entry.Contract=="DefectCandidates"{defects=append(defects,entry)}
  if entry.Contract=="ResolvedStyle"{resolved=append(resolved,entry)}
  if entry.Contract=="ResolvedStyle"||entry.Contract=="Override"{inline=append(inline,entry)}
  if entry.Contract=="Params"{
   node,readErr:=jsonResolver.ResolveExportNode(ctx,entry);if readErr!=nil{return nil,nil,nil,nil,nil,readErr}
   if node.Entry!=entry{return nil,nil,nil,nil,nil,v2input.ErrInvalid}
   masks,extractErr:=exportParamsInlineMasks(entry,node.Body);if extractErr!=nil{return nil,nil,nil,nil,nil,extractErr}
   if len(masks)>0{inline=append(inline,entry)}
  }
  if entry.Contract=="AbsoluteLUTArray"{hasLUT=true}
 }
 var luts []v2input.Entry;if hasLUT{luts=append(luts,resolved...)}
 return parents,defects,luts,inline,reports,ctx.Err()
}

func resolveExportInputInventory(ctx context.Context,compiled CompiledExportPlan,resolver ExportClosureNodeResolver,discoverOnly bool)(ExportInputInventory,error){
 var zero ExportInputInventory
 if err:=ctx.Err();err!=nil{return zero,err}
 if resolver==nil||compiled.task==uuid.Nil||len(compiled.roots)==0{return zero,v2input.ErrInvalid}
 var plan importPlanDocument
 if err:=decodeImportDocument(compiled.planBody,&plan);err!=nil{return zero,err}
 validator:=ExportPlanDocumentValidator{Task:compiled.task,Project:plan.Project,SnapshotDigest:plan.Snapshot,Plan:compiled.planRef}
 for _,document:=range []struct{entry v2input.Entry;body []byte}{{v2input.Entry{Ref:compiled.planRef,Contract:"InterchangePlan"},compiled.planBody},{v2input.Entry{Ref:compiled.paramsRef,Contract:"Params"},compiled.paramsBody}}{
  if _,err:=validator.ExportDocumentChildren(ctx,document.entry,document.body);err!=nil{return zero,err}
 }
 seen:=map[uuid.UUID]v2input.Entry{};active:=map[uuid.UUID]bool{};entries:=[]v2input.Entry{};jsonBytes:=0
 binaries:=map[string]bool{"EncodedImage":true,"StyleBackgroundAsset":true,"WorkingPixels":true,"WorkingAlpha":true,"PixelArray":true,"MaskPixels":true,"MaskArray":true,"DenseWarpField":true,"DenseWarpArray":true,"DefectLabelMap":true,"DefectLabelPNG":true,"AbsoluteLUTArray":true}
 var visit func(v2input.Entry,int)error
 visit=func(entry v2input.Entry,depth int)error{
  if err:=ctx.Err();err!=nil{return err}
  if depth>32||!validImportRef(entry.Ref)||len(entry.Contract)<1||len(entry.Contract)>120{return v2input.ErrInvalid}
  if old,ok:=seen[entry.Ref.ArtifactID];ok{if old!=entry||active[entry.Ref.ArtifactID]{return v2input.ErrInvalid};return nil}
  if len(seen)>=v2input.MaxEntries{return v2input.ErrInvalid}
  seen[entry.Ref.ArtifactID]=entry;active[entry.Ref.ArtifactID]=true
  node:=ExportClosureNode{Entry:entry}
  if !discoverOnly||!binaries[entry.Contract]{
   resolved,err:=resolver.ResolveExportNode(ctx,entry);if err!=nil{return err};node=resolved
  }
  // Own resolver buffers before any further callbacks.
  body,children:=append([]byte(nil),node.Body...),append([]v2input.Entry(nil),node.Children...)
  if err:=ctx.Err();err!=nil{return err}
  if node.Entry!=entry{return v2input.ErrInvalid}
  if node.Body==nil{
   if !binaries[entry.Contract]||len(children)!=0{return v2input.ErrInvalid}
  }else{
   if binaries[entry.Contract]||len(body)<2||len(body)>8<<20||jsonBytes+len(body)>128<<20||rawSHA(body)!=entry.Ref.SHA256{return v2input.ErrInvalid}
   jsonBytes+=len(body)
   normalized,err:=canonical.Bytes(body);if err!=nil||!bytes.Equal(normalized,body){return v2input.ErrInvalid}
   if entry.Ref==compiled.planRef&&!bytes.Equal(body,compiled.planBody)||entry.Ref==compiled.paramsRef&&!bytes.Equal(body,compiled.paramsBody){return v2input.ErrInvalid}
   refs,err:=importDocumentRefs(body,entry.Contract);if err!=nil{return err}
   supplied:=map[uuid.UUID]v2input.Entry{}
   for _,child:=range children{if old,ok:=supplied[child.Ref.ArtifactID];ok&&old!=child{return v2input.ErrInvalid};supplied[child.Ref.ArtifactID]=child}
   if len(refs)!=len(supplied){return v2input.ErrInvalid}
   for id,ref:=range refs{if supplied[id].Ref!=ref{return v2input.ErrInvalid}}
   if entry.Ref==compiled.paramsRef||entry.Ref==compiled.planRef{
    expected,err:=validator.ExportDocumentChildren(ctx,entry,body);if err!=nil{return err}
    if len(expected)!=len(supplied){return v2input.ErrInvalid}
    for _,child:=range expected{if supplied[child.Ref.ArtifactID]!=child{return v2input.ErrInvalid}}
   }
  }
  for _,child:=range children{if err:=visit(child,depth+1);err!=nil{return err}}
  active[entry.Ref.ArtifactID]=false;entries=append(entries,entry);return nil
 }
 for _,root:=range compiled.roots{if err:=visit(root,0);err!=nil{return zero,err}}
 sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 digest,err:=canonical.Hash(entries);if err!=nil{return zero,err}
 return ExportInputInventory{task:compiled.task,entries:entries,digest:digest},ctx.Err()
}
