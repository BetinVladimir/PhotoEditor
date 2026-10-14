import {describe,it,expect,vi} from 'vitest';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {readMultiPreview,type MultiPreview} from './multi-preview';
import {downloadMultiPreview} from './multi-preview-download';
import type {MultiProposal} from './multi-proposal';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:MultiProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),operation:'optical.panorama',sources:[{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},{asset_version_id:id(12),source:ref(13),canvas_hash:'b'.repeat(64)}],reference_ordinal:0,registrations:[ref(14),ref(15)],reason:'PANORAMA_REVIEW;motion_fraction=0',working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'multi:'+ 'c'.repeat(32),source_sha256:'c'.repeat(64)},status:'review',requires_review:true};
const bytes=new Uint8Array([1,2,3]);
function fixture():MultiPreview{return {schema_version:1,tenant_id:id(1),project_id:id(2),operation:'optical.panorama',producer_task_id:id(3),result_id:id(4),variant:'preview1600',artifact:{artifact_id:id(11),sha256:bytesToHex(sha256(bytes))},bytes:3,media_type:'image/webp',width:100,height:80,url:'https://objects.example/preview?versionId=immutable',expires_at:new Date(Date.now()+60000).toISOString()};}
describe('multi encoded preview',()=>{

 it('owns current-result descriptor',()=>{const value=fixture(),owned=readMultiPreview(value,proposal,'preview1600');value.artifact.sha256='b'.repeat(64);expect(owned.artifact.sha256).toBe(bytesToHex(sha256(bytes)));});
 it('rejects scope substitution, role alias, expiry, unversioned URL and dimensions',()=>{
  for(const patch of [{tenant_id:id(12)},{result_id:id(12)},{operation:'optical.focus_stack'},{artifact:ref(8)},{expires_at:new Date(0).toISOString()},{url:'https://objects.example/preview'},{url:'http://objects.example/preview?versionId=v1'},{url:'https://objects.example/preview?versionId=v1&versionId=v2'},{width:1601},{url:'file:///tmp/x?versionId=v'},{bucket:'private'}])expect(()=>readMultiPreview({...fixture(),...patch},proposal,'preview1600')).toThrow();
  expect(readMultiPreview({...fixture(),url:'http://127.0.0.1:9000/preview?versionId=immutable'},proposal,'preview1600').url).toBe('http://127.0.0.1:9000/preview?versionId=immutable');
 });
 it('streams exact bytes without credentials',async()=>{
  const fetcher=vi.fn(async(_input:RequestInfo|URL,_init?:RequestInit)=>new Response(bytes,{status:200,headers:{'content-type':'image/webp','content-length':'3'}}));
  const controller=new AbortController();expect(await downloadMultiPreview(fixture(),controller.signal,fetcher as typeof fetch)).toEqual(bytes);
  expect(fetcher.mock.calls[0]?.[1]).toMatchObject({credentials:'omit',redirect:'error',cache:'no-store',signal:controller.signal});
 });
 it('refuses ambiguous object versions before download',async()=>{
  const preview={...fixture(),url:'https://objects.example/preview?versionId=v1&versionId=v2'};
  const fetcher=vi.fn();
  await expect(downloadMultiPreview(preview,new AbortController().signal,fetcher)).rejects.toThrow('MULTI_PREVIEW_INVALID');
  expect(fetcher).not.toHaveBeenCalled();
 });
 it('refuses remote HTTP before download but accepts local object storage',async()=>{
  const fetcher=vi.fn(async()=>new Response(bytes,{headers:{'content-type':'image/webp'}}));
  await expect(downloadMultiPreview({...fixture(),url:'http://objects.example/preview?versionId=v1'},new AbortController().signal,fetcher as typeof fetch)).rejects.toThrow('MULTI_PREVIEW_INVALID');
  expect(fetcher).not.toHaveBeenCalled();
  expect(await downloadMultiPreview({...fixture(),url:'http://127.0.0.1:9000/preview?versionId=v1'},new AbortController().signal,fetcher as typeof fetch)).toEqual(bytes);
 });
 it('refuses repinned encoding bytes and overflowing stream',async()=>{
  for(const body of [new Uint8Array([1,2,4]),new Uint8Array([1,2,3,4])]){
   const fetcher=async()=>new Response(body,{headers:{'content-type':'image/webp'}});
   await expect(downloadMultiPreview(fixture(),new AbortController().signal,fetcher as typeof fetch)).rejects.toThrow();
  }
 });
 it('stopped work never invokes transport',async()=>{
  const controller=new AbortController();controller.abort();const transport=vi.fn();
  await expect(downloadMultiPreview(fixture(),controller.signal,transport)).rejects.toThrow('ABORTED');expect(transport).not.toHaveBeenCalled();
 });
});
