import {canonicalJSON} from '@photoeditor/api-client';
import {freezeRegionRequest,type RegionPreviewRequest} from './region-preview';
import {regionGeometry} from './region-preview-view';
import type {RegionManifestBinding} from './region-preview-manifest';
function exact(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));}
const hash=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
/** Only accept this descriptor from the authorized admitted-job read endpoint. */
export function readRegionDescriptor(value:unknown,asset:string,request:RegionPreviewRequest,source:{artifact_id:string;sha256:string}):RegionManifestBinding {
 const expected=freezeRegionRequest(request);
 if(!uuid(asset)||!uuid(source.artifact_id)||!hash(source.sha256)||!exact(value,['schema_version','asset_id','request','source','canvas','params_sha256','icc_sha256','alpha',...(value&&typeof value==='object'&&Object.hasOwn(value,'execution')?['execution']:[])])||value.schema_version!==1||value.asset_id!==asset||!exact(value.source,['artifact_id','sha256'])||canonicalJSON(value.source)!==canonicalJSON(source)||!exact(value.canvas,['width','height'])||!hash(value.params_sha256)||!hash(value.icc_sha256)||typeof value.alpha!=='boolean')throw new Error('REGION_DESCRIPTOR_INVALID');
 const admitted=freezeRegionRequest(value.request as RegionPreviewRequest);
 if(canonicalJSON(admitted)!==canonicalJSON(expected))throw new Error('REGION_DESCRIPTOR_INVALID');
 const canvas={width:value.canvas.width as number,height:value.canvas.height as number};regionGeometry(canvas.width,canvas.height,expected.rect,expected.max_edge);
 let execution:RegionManifestBinding['execution'];
 if(Object.hasOwn(value,'execution')){
  const pin=value.execution;
  if(!exact(pin,['sha256','target_canvas'])||!hash(pin.sha256)||!exact(pin.target_canvas,['width','height','orientation','space_id','source_sha256'])||pin.target_canvas.orientation!==1||typeof pin.target_canvas.space_id!=='string'||!pin.target_canvas.space_id||!hash(pin.target_canvas.source_sha256)||!Number.isSafeInteger(pin.target_canvas.width)||!Number.isSafeInteger(pin.target_canvas.height)||(pin.target_canvas.width as number)<1||(pin.target_canvas.height as number)<1||(pin.target_canvas.width as number)*(pin.target_canvas.height as number)>200000000)throw new Error('REGION_DESCRIPTOR_INVALID');
  execution={sha256:pin.sha256,target_canvas:{...pin.target_canvas}};
 }
 return {...(execution?{execution}:{}),request:expected,source:{...source},canvas,paramsSha256:value.params_sha256,iccSha256:value.icc_sha256,alpha:value.alpha};
}
