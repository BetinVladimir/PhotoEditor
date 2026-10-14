import type {V2VersionBinding} from './catalog-types';
import {freezeMultiIntent,isMultiBinding,readMultiCapabilities,type MultiCapabilities,type MultiIntent} from './multi-intent';
/** Editable ordered selection. Reference follows its version when frames move. */
export class MultiSelection {
 private capabilities:MultiCapabilities;
 private frames:V2VersionBinding[]=[];
 private reference:string|null=null;
 private projection:'planar'|'cylindrical'='planar';
 private motion:'review'|'reject'='review';
 constructor(capabilities:MultiCapabilities){this.capabilities=readMultiCapabilities(capabilities,capabilities.tenant_id,capabilities.project_id,capabilities.operation);}
 get snapshot(){return {frames:structuredClone(this.frames),reference_version:this.reference,projection:this.projection,motion_policy:this.motion};}
 add(binding:V2VersionBinding){
  if(!isMultiBinding(binding)||binding.source.artifact_id===this.capabilities.calibration.artifact_id||this.frames.length>=this.capabilities.frame_limit||this.frames.some(f=>f.asset_version_id===binding.asset_version_id))throw new Error('MULTI_SELECTION_INVALID');
  this.frames.push(structuredClone(binding));if(this.reference===null)this.reference=binding.asset_version_id;
 }
 remove(version:string){
  const index=this.frames.findIndex(f=>f.asset_version_id===version);if(index<0)throw new Error('MULTI_SELECTION_INVALID');
  this.frames.splice(index,1);if(this.reference===version)this.reference=this.frames[0]?.asset_version_id??null;
 }
 move(version:string,to:number){
  const from=this.frames.findIndex(f=>f.asset_version_id===version);
  if(from<0||!Number.isInteger(to)||to<0||to>=this.frames.length)throw new Error('MULTI_SELECTION_INVALID');
  const [frame]=this.frames.splice(from,1);this.frames.splice(to,0,frame);
 }
 selectReference(version:string){if(!this.frames.some(f=>f.asset_version_id===version))throw new Error('MULTI_SELECTION_INVALID');this.reference=version;}
 setProjection(value:'planar'|'cylindrical'){if(this.capabilities.operation!=='optical.panorama'||!['planar','cylindrical'].includes(value))throw new Error('MULTI_SELECTION_INVALID');this.projection=value;}
 setMotionPolicy(value:'review'|'reject'){if(this.capabilities.operation!=='optical.focus_stack'||!['review','reject'].includes(value))throw new Error('MULTI_SELECTION_INVALID');this.motion=value;}
 build(snapshot:string):MultiIntent{
  const common={schema_version:1 as const,frames:this.frames,reference_ordinal:this.frames.findIndex(f=>f.asset_version_id===this.reference),max_pixels:this.capabilities.max_pixels,calibration:this.capabilities.calibration};
  return freezeMultiIntent(this.capabilities.operation,{schema_version:1,snapshot_id:snapshot,params:this.capabilities.operation==='optical.panorama'?{...common,projection:this.projection}:{...common,motion_policy:this.motion}});
 }
}
