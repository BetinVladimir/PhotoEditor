import {it,expect} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {verifyRegionManifest,type RegionManifestBinding} from './region-preview-manifest';
import type {RunOutput} from './catalog-types';
const id=(n:number)=>`${n.toString().repeat(8)}-${n.toString().repeat(4)}-4${n.toString().repeat(3)}-8${n.toString().repeat(3)}-${n.toString().repeat(12)}`;
const binding:RegionManifestBinding={request:{version_id:id(1),recipe:{artifact_id:id(2),sha256:'a'.repeat(64)},rect:{x0:0,y0:0,x1:1,y1:1},max_edge:1024},source:{artifact_id:id(3),sha256:'b'.repeat(64)},canvas:{width:6000,height:4000},paramsSha256:'c'.repeat(64),iccSha256:'d'.repeat(64),alpha:false};
const digest=(text:string)=>bytesToHex(sha256(new TextEncoder().encode(text)));
function fixture(){
 const derivative:RunOutput={artifact_id:id(4),sha256:'e'.repeat(64),role:'derivative',bytes:1000,media_type:'image/jpeg',url:'https://photo.test/image',expires_at:'2099-01-01'};
 const value={schema_version:1,params_sha256:binding.paramsSha256,asset_version_id:binding.request.version_id,source:binding.source,recipe:binding.request.recipe,rect:binding.request.rect,geometry:{left:0,top:0,width:6000,height:4000,output_width:1024,output_height:682},derivative:{artifact_id:derivative.artifact_id,sha256:derivative.sha256},bytes:1000,icc_sha256:binding.iccSha256,cache_key:digest(canonicalJSON({schema_version:1,params:binding.paramsSha256,target_icc_sha256:binding.iccSha256,quality:88,format:'jpeg',metadata_policy:'stripped',resize:'area_linear_premult_v1'})),media_type:'image/jpeg',alpha:false,metadata_policy:'stripped',quality:88};
 return {value,derivative};
}
function verify(raw:string,derivative:RunOutput){const body=new TextEncoder().encode(raw);const document:RunOutput={artifact_id:id(5),sha256:digest(raw),bytes:body.length,role:'document',media_type:'application/json',url:'https://photo.test/json',expires_at:'2099-01-01'};return verifyRegionManifest(body,document,derivative,binding);}
it('binds a canonical region document to all requested evidence',()=>{const f=fixture();expect(verify(canonicalJSON(f.value),f.derivative)).toMatchObject({geometry:f.value.geometry,cacheKey:f.value.cache_key,bytes:1000});});
it('rejects substituted source, recipe, canvas, profile, size and cache evidence even with valid document hash',()=>{
 const f=fixture();const mutations={asset_version_id:id(6),source:{...binding.source,sha256:'f'.repeat(64)},recipe:{...binding.request.recipe,artifact_id:id(6)},rect:{...binding.request.rect,x1:0.5},geometry:{...f.value.geometry,output_height:683},icc_sha256:'f'.repeat(64),params_sha256:'f'.repeat(64),cache_key:'f'.repeat(64),alpha:true,bytes:1001,quality:89,metadata_policy:'preserved',media_type:'image/tiff',extra:true};
 for(const [key,value] of Object.entries(mutations))expect(()=>verify(canonicalJSON({...f.value,[key]:value}),f.derivative),key).toThrow('REGION_MANIFEST_INVALID');
});
it('rejects noncanonical and duplicate JSON and mismatched catalog derivative',()=>{
 const f=fixture(),raw=canonicalJSON(f.value);expect(()=>verify(raw+' ',f.derivative)).toThrow();expect(()=>verify('\ufeff'+raw,f.derivative)).toThrow();expect(()=>verify(raw.replace('{','{"alpha":true,'),f.derivative)).toThrow();expect(()=>verify(raw,{...f.derivative,artifact_id:id(6)})).toThrow();expect(()=>verify(raw,{...f.derivative,sha256:'f'.repeat(64)})).toThrow();
 const body=new TextEncoder().encode(raw);expect(()=>verifyRegionManifest(body,{bytes:body.length,sha256:'f'.repeat(64),role:'document',media_type:'application/json'} as RunOutput,f.derivative,binding)).toThrow();
});
