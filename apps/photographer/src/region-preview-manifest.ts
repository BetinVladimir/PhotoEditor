import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {RunOutput} from './catalog-types';
import {freezeRegionRequest,type RegionPreviewRequest} from './region-preview';
import {regionGeometry,type RegionGeometry} from './region-preview-view';
type Ref={artifact_id:string;sha256:string};
export type RegionManifestBinding={request:RegionPreviewRequest;source:Ref;canvas:{width:number;height:number};paramsSha256:string;iccSha256:string;alpha:boolean};
export type VerifiedRegionManifest={geometry:RegionGeometry;cacheKey:string;derivative:Ref;mediaType:'image/jpeg'|'image/webp';bytes:number};
const digest=(bytes:Uint8Array)=>bytesToHex(sha256(bytes));
const hash=(value:unknown)=>typeof value==='string'&&/^[0-9a-f]{64}$/.test(value);
function exact(value:unknown,keys:string[]):value is Record<string,unknown>{return !!value&&typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).length===keys.length&&keys.every(key=>Object.hasOwn(value,key));}
function reference(value:unknown):value is Ref{return exact(value,['artifact_id','sha256'])&&typeof value.artifact_id==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value.artifact_id)&&value.artifact_id!=='00000000-0000-0000-0000-000000000000'&&hash(value.sha256);}
/** Verify the actual canonical document bytes, not an untrusted parsed response. */
export function verifyRegionManifest(body:Uint8Array,document:RunOutput,derivative:RunOutput,binding:RegionManifestBinding):VerifiedRegionManifest {
 const fail=()=>{throw new Error('REGION_MANIFEST_INVALID');};
 const request=freezeRegionRequest(binding.request);
 if(!reference(binding.source)||!hash(binding.paramsSha256)||!hash(binding.iccSha256)||typeof binding.alpha!=='boolean'||body.byteLength<2||body.byteLength>8388608||document.role!=='document'||document.media_type!=='application/json'||body.byteLength!==document.bytes||digest(body)!==document.sha256)fail();
 let raw:string,value:unknown;
 try{raw=new TextDecoder('utf-8',{fatal:true,ignoreBOM:true}).decode(body);value=JSON.parse(raw);if(canonicalJSON(value)!==raw)fail();}catch{fail();}
 if(!exact(value,['schema_version','params_sha256','asset_version_id','source','recipe','rect','geometry','derivative','bytes','icc_sha256','cache_key','media_type','alpha','metadata_policy','quality']))return fail();
 const geometry=regionGeometry(binding.canvas.width,binding.canvas.height,request.rect,request.max_edge);
 if(value.schema_version!==1||value.params_sha256!==binding.paramsSha256||value.asset_version_id!==request.version_id||!reference(value.source)||!reference(value.recipe)||!reference(value.derivative)||canonicalJSON(value.source)!==canonicalJSON(binding.source)||canonicalJSON(value.recipe)!==canonicalJSON(request.recipe)||canonicalJSON(value.rect)!==canonicalJSON(request.rect)||canonicalJSON(value.geometry)!==canonicalJSON(geometry)||value.icc_sha256!==binding.iccSha256||value.alpha!==binding.alpha||value.metadata_policy!=='stripped'||value.quality!==88||value.media_type!==(binding.alpha?'image/webp':'image/jpeg'))return fail();
 const expectedKey=digest(new TextEncoder().encode(canonicalJSON({schema_version:1,params:binding.paramsSha256,target_icc_sha256:binding.iccSha256,quality:88,format:binding.alpha?'webp':'jpeg',metadata_policy:'stripped',resize:'area_linear_premult_v1'})));
 if(value.cache_key!==expectedKey||derivative.role!=='derivative'||value.derivative.artifact_id!==derivative.artifact_id||value.derivative.sha256!==derivative.sha256||value.derivative.artifact_id===document.artifact_id||value.derivative.artifact_id===binding.source.artifact_id||value.derivative.artifact_id===request.recipe.artifact_id||value.bytes!==derivative.bytes||!Number.isSafeInteger(value.bytes)||derivative.bytes<2||derivative.bytes>16777216||value.media_type!==derivative.media_type)return fail();
 return {geometry,cacheKey:expectedKey,derivative:{...value.derivative},mediaType:value.media_type as 'image/jpeg'|'image/webp',bytes:derivative.bytes};
}
