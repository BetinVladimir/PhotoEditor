import {expect,it,vi} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {readLegacyRunAccepted,readLegacyMaskAccepted,readLegacyOverride,readGenerationAbandonment} from './legacy-producer-dto';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,source='a'.repeat(64);
const run=()=>({run_id:id(1),status:'queued'});
const draft=()=>({asset_version_id:id(2),revision:'1',source_sha256:source,params:{schema_version:1,modules:{strength:.7}},mask_refs:[]});
const override=()=>{const config={schema_version:1,input_sha256:source,revision:4,modules:{strength:.7}};return {id:id(3),asset_version_id:id(2),input_sha256:source,revision:'4',config,config_hash:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(config)))),created_at:'2026-10-17T00:00:00Z'};};
it('distinguishes exact immutable run and source-bound mask receipts',()=>{
 expect(readLegacyRunAccepted(run())).toEqual(run());expect(readLegacyMaskAccepted({...run(),source_sha256:source},source)).toEqual({...run(),source_sha256:source});
 for(const value of [{run_id:'bad',status:'queued'},{...run(),status:'running'},{...run(),extra:true},{...run(),source_sha256:source}])expect(()=>readLegacyRunAccepted(value)).toThrow();
 for(const value of [run(),{...run(),source_sha256:'b'.repeat(64)},{...run(),source_sha256:source,extra:true}])expect(()=>readLegacyMaskAccepted(value,source)).toThrow();
});
it('checks exact override source, decimal/config revision and content digest, preserving normalized revision',()=>{
 const value=override(),owned=readLegacyOverride(value,id(2),source);value.config.modules.strength=.2;expect(owned.config).toMatchObject({revision:4,modules:{strength:.7}});
 expect(()=>readLegacyOverride(override(),id(2),source,{schema_version:1,input_sha256:source,revision:1,modules:{strength:.2}})).toThrow('EDITOR_OVERRIDE_PARAMS_CHANGED');
 for(const change of [{asset_version_id:id(9)},{input_sha256:'b'.repeat(64)},{revision:'04'},{revision:'5'},{config_hash:'b'.repeat(64)},{config:{...override().config,modules:{strength:.2}}},{created_at:'never'},{extra:true}])expect(()=>readLegacyOverride({...override(),...change},id(2),source)).toThrow();
});
it('binds abandonment tombstones to the exact original key and accepts committed run recovery',()=>{
 expect(readGenerationAbandonment({status:'abandoned',key:'key'},'key')).toEqual({status:'abandoned',key:'key'});expect(readGenerationAbandonment(run(),'key')).toEqual(run());
 for(const value of [{status:'abandoned',key:'other'},{status:'abandoned',key:'key',run_id:id(1)},{}])expect(()=>readGenerationAbandonment(value,'key')).toThrow();
});
it('uses frozen mask source while awaiting and refuses a cross-source transport reply',async()=>{
 let finish!:(value:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{finish=done;})),api=new OwnerAPI({request} as never,id(4)),input=draft();
 const pending=api.promptMask(input,[{x:.2,y:.3,label:'positive'}]);input.source_sha256='b'.repeat(64);finish({...run(),source_sha256:'b'.repeat(64)});await expect(pending).rejects.toThrow('EDITOR_MASK_RECEIPT_INVALID');
});
it.each(['generate','abandon'] as const)('freezes %s body and refuses noncooperative cancellation',async kind=>{
 let finish!:(value:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{finish=done;})),api=new OwnerAPI({request} as never,id(4)),controller=new AbortController(),input=draft(),mask={artifact:{artifact_id:id(5),sha256:source},protected:false} as never;
 const invoke=()=>kind==='generate'?api.generate(input,mask,'chosen',7,1,'same-key',controller.signal):api.abandonGeneration(input,mask,'chosen',7,1,'same-key',controller.signal);
 const pending=invoke();input.mask_refs.push({} as never);controller.abort();finish(run());await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(invoke()).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
it('refuses an invalid source version before any interactive transport',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(4)),input={...draft(),asset_version_id:'invalid'},mask={artifact:{artifact_id:id(5),sha256:source},protected:false} as never;
 const calls=[()=>api.preview(input),()=>api.promptMask(input,[{x:.2,y:.3,label:'positive'}]),()=>api.manualMask(input,[]),()=>api.createOverride(input),()=>api.generate(input,mask,'chosen',7,1,'same-key'),()=>api.abandonGeneration(input,mask,'chosen',7,1,'same-key')];
 for(const call of calls)await expect(call()).rejects.toThrow('SCOPE_INVALID');
 expect(request).not.toHaveBeenCalled();
});
it.each(['preview','prompt','manual','override'] as const)('refuses a noncooperative %s response after abort',async kind=>{
 let finish!:(value:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{finish=done;})),api=new OwnerAPI({request} as never,id(4)),controller=new AbortController(),input=draft();
 const invoke=()=>kind==='preview'?api.preview(input,'proxy',controller.signal):kind==='prompt'?api.promptMask(input,[{x:.2,y:.3,label:'positive'}],controller.signal):kind==='manual'?api.manualMask(input,[],[],controller.signal):api.createOverride(input,controller.signal);
 const pending=invoke();controller.abort();finish(kind==='override'?override():kind==='preview'?run():{...run(),source_sha256:source});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});await expect(invoke()).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
