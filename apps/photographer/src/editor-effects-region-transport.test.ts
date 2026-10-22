import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const requestBody=()=>({version_id:id(3),recipe:{artifact_id:id(4),sha256:'a'.repeat(64)},rect:{x0:0,y0:0,x1:1,y1:1},max_edge:512});
const frozen=()=>({document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)}});
it('refuses malformed paths and cancelled Effects/Region calls before any I/O',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(9)),abort=new AbortController();abort.abort();
 for(const call of [()=>api.editorEffectsStatus('bad',id(6)),()=>api.editorEffectsStatus(id(1),'bad'),()=>api.queueEditorEffects('bad',frozen(),id(6)),()=>api.regionPreview('bad',requestBody(),id(6)),()=>api.regionDescriptor(id(1),'bad',requestBody(),{artifact_id:id(4),sha256:'a'.repeat(64)})])await expect(call()).rejects.toThrow();
 for(const call of [()=>api.editorEffectsStatus(id(1),id(6),abort.signal),()=>api.queueEditorEffects(id(1),frozen(),id(6),abort.signal),()=>api.regionPreview(id(1),requestBody(),id(6),abort.signal),()=>api.regionDescriptor(id(1),id(6),requestBody(),{artifact_id:id(4),sha256:'a'.repeat(64)},abort.signal)])await expect(call()).rejects.toMatchObject({name:'AbortError'});
 expect(request).not.toHaveBeenCalled();
});
it('freezes Region request before transport and withholds a late accepted ticket after Stop',async()=>{
 const body=requestBody(),expected=structuredClone(body),abort=new AbortController();
 const request=vi.fn(async(_path,options)=>{body.rect.x1=.5;body.recipe.sha256='d'.repeat(64);expect(options.body).toEqual(expected);expect(options.idempotencyKey).toBe(id(7));abort.abort();return {run_id:id(6),workflow_id:id(8),state:'queued'}});
 const api=new OwnerAPI({request} as never,id(9));await expect(api.regionPreview(id(1),body,id(7),abort.signal)).rejects.toMatchObject({name:'AbortError'});
 expect(request).toHaveBeenCalledOnce();
});
it('does not expose Effects status or accepted reply after cancellation',async()=>{
 for(const status of [false,true]){const abort=new AbortController(),request=vi.fn(async()=>{abort.abort();return status?{build_run_id:id(6),build_status:'running',state:'building',seal_run_id:null,final_run_id:null,review_required:false}:{run_id:id(6),output_collection_id:'00000000-0000-0000-0000-000000000000',status:'queued'}}),api=new OwnerAPI({request} as never,id(9));await expect(status?api.editorEffectsStatus(id(1),id(6),abort.signal):api.queueEditorEffects(id(1),frozen(),id(7),abort.signal)).rejects.toMatchObject({name:'AbortError'});}
});
it('requires the same valid idempotency key before Region admission',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(9));for(const key of ['', 'with space', 'x'.repeat(129)])await expect(api.regionPreview(id(1),requestBody(),key)).rejects.toThrow('IDEMPOTENCY_KEY_REQUIRED');expect(request).not.toHaveBeenCalled();
});
it('does not expose a late Region descriptor after cancellation',async()=>{
 const abort=new AbortController(),body=requestBody(),source={artifact_id:id(4),sha256:'a'.repeat(64)};
 const request=vi.fn(async()=>{abort.abort();return {schema_version:1,asset_id:id(1),request:body,source,canvas:{width:100,height:100},params_sha256:'b'.repeat(64),icc_sha256:'c'.repeat(64),alpha:false}}),api=new OwnerAPI({request} as never,id(9));
 await expect(api.regionDescriptor(id(1),id(6),body,source,abort.signal)).rejects.toMatchObject({name:'AbortError'});
});
