package delivery
import (
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)
// CompileWatermarkPrepareProof requires a complete immutable pin inventory.
// SQL must independently reauthorize all sources before publishing/admission.
func CompileWatermarkPrepareProof(scope WatermarkPinScope,session uuid.UUID,frozenRef v2input.Ref,frozenRaw,snapshotRaw []byte,configs []PresetContent,pins []PreparedWatermarkPin)(PrepareProof,[]byte,[]byte,error){
 if scope.Tenant==uuid.Nil||scope.Project==uuid.Nil||scope.Actor==uuid.Nil||scope.Parent==uuid.Nil||len(pins)<1||len(pins)>16{return PrepareProof{},nil,nil,v2input.ErrInvalid}
 selected:=make(map[uuid.UUID]PreparedWatermarkPin,len(pins))
 for _,pin:=range pins{if _,ok:=selected[pin.Preset];ok{return PrepareProof{},nil,nil,v2input.ErrInvalid};selected[pin.Preset]=pin}
 for _,item:=range configs{
  config,err:=exportpreset.DecodeFrozenConfig(item.Raw,item.SHA);if err!=nil{return PrepareProof{},nil,nil,err}
  pin,found:=selected[item.ID]
  if config.Watermark==nil{if found{return PrepareProof{},nil,nil,v2input.ErrInvalid};continue}
  if !found||pin.PresetSHA!=item.SHA{return PrepareProof{},nil,nil,v2input.ErrInvalid}
  exact:=scope;exact.Preset=item.ID;exact.PresetSHA=item.SHA
  if _,err:=DecodeWatermarkSourcePin(pin.Pin.JCS(),pin.Pin.SHA256(),exact,item.Raw);err!=nil{return PrepareProof{},nil,nil,err}
  delete(selected,item.ID)
 }
 if len(selected)!=0{return PrepareProof{},nil,nil,v2input.ErrInvalid}
 return compilePrepareProof(scope.Project,session,frozenRef,frozenRaw,snapshotRaw,configs,true)
}
func CompileWatermarkPreparePlan(scope WatermarkPinScope,session uuid.UUID,paramsRef,frozenRef v2input.Ref,frozenRaw,snapshotRaw []byte,configs []PresetContent,pins []PreparedWatermarkPin)(PreparePlan,error){
 if _,_,_,err:=CompileWatermarkPrepareProof(scope,session,frozenRef,frozenRaw,snapshotRaw,configs,pins);err!=nil{return PreparePlan{},err}
 workflow,err:=v2input.WorkflowID(scope.Parent);if err!=nil{return PreparePlan{},err}
 plan,err:=compilePreparePlan(workflow,scope.Project,session,paramsRef,frozenRef,frozenRaw,snapshotRaw,configs,true)
 if err!=nil{return PreparePlan{},err}
 // Require immutable watermark dependencies in assembly independently of the
 // SQL inventory. These roots do not alter the ordinary prepare task closure.
 for _,prepared:=range pins{
  var envelope watermarkPinEnvelope
  if json.Unmarshal(prepared.Pin.JCS(),&envelope)!=nil{return PreparePlan{},v2input.ErrInvalid}
  plan.watermarkAssemblyRoots=append(plan.watermarkAssemblyRoots,v2input.Entry{Ref:envelope.Binding.Source,Contract:"WorkingImage"})
  for _,array:=range envelope.Arrays{plan.watermarkAssemblyRoots=append(plan.watermarkAssemblyRoots,v2input.Entry{Ref:array.Ref,Contract:array.Contract})}
 }
 return plan,nil
}
