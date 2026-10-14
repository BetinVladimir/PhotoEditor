package v2input

import "github.com/google/uuid"

// CompileSignedDeliveryAssemblyClosure builds the task input closure for the
// separate required-provenance root. The caller must derive requiredRoots from
// canonical envelope, assembly, shards and signing closure bytes, then resolve
// every artifact under current parent/producer authority before calling this.
func CompileSignedDeliveryAssemblyClosure(workflow uuid.UUID,params DeliveryAssembleParams,
 paramsRef Ref,requiredRoots,resolved []Entry)(CaptureClosure,error){
 if params.SchemaVersion!=1||!validRef(params.Assembly)||!validRef(paramsRef)||
  len(requiredRoots)<7||len(requiredRoots)>1000000||len(resolved)>1000000{return CaptureClosure{},ErrInvalid}
 task,err:=TaskID(workflow,"v2.delivery.assemble",0)
 if err!=nil{return CaptureClosure{},err}
 inventory:=make(map[uuid.UUID]Entry,len(resolved))
 for _,entry:=range resolved{
  if !validEntry(entry){return CaptureClosure{},ErrInvalid}
  if previous,ok:=inventory[entry.Ref.ArtifactID];ok&&previous!=entry{return CaptureClosure{},ErrInvalid}
  inventory[entry.Ref.ArtifactID]=entry
 }
 root:=Entry{Ref:params.Assembly,Contract:"SignedDeliveryAssemblyEnvelope"}
 if inventory[params.Assembly.ArtifactID]!=root{return CaptureClosure{},ErrInvalid}
 seen:=make(map[uuid.UUID]bool,len(requiredRoots))
 counts:=map[string]int{}
 for _,entry:=range requiredRoots{
  if !validEntry(entry)||inventory[entry.Ref.ArtifactID]!=entry||seen[entry.Ref.ArtifactID]{return CaptureClosure{},ErrInvalid}
  seen[entry.Ref.ArtifactID]=true
  counts[entry.Contract]++
 }
 if counts[root.Contract]!=1||counts["DeliveryAssemblyInput"]!=1||
  counts["SignedDeliveryAssemblyClosure"]!=1||counts["DeliverySlotShard"]<1||
  counts["DeliverySlotShard"]>160||counts["ExportPresetConfig"]<1||
  counts["ExportPresetConfig"]>16||counts["SignedExport"]<1||
  counts["SignedExport"]!=counts["C2PAManifest"]||
  counts["EncodedExport"]!=counts["SignedExport"]{return CaptureClosure{},ErrInvalid}
 entries:=append([]Entry(nil),resolved...)
 entries=append(entries,Entry{Ref:paramsRef,Contract:"Params"})
 closure,err:=compileCaptureInventory(task,"v2.delivery.assemble",params,paramsRef,entries)
 if err!=nil{return CaptureClosure{},err}
 closure.Direct=[]Ref{paramsRef,params.Assembly}
 return closure,nil
}
