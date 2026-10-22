import {expect,it,vi} from 'vitest';
import {readLegacyEditorDraft} from './legacy-editor-dto';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const draft=()=>({asset_version_id:id(1),revision:'0',params:{modules:{strength:1}},mask_refs:[],source_sha256:'a'.repeat(64)});
it('owns actual legacy draft shape and refuses foreign source, arbitrary masks and noncanonical revisions',()=>{
 const source=draft(),owned=readLegacyEditorDraft(source);source.params.modules.strength=.5;expect(owned.params).toEqual({modules:{strength:1}});
 for(const changed of [{mask_refs:{}},{revision:'01'},{revision:'9223372036854775808'},{last_preview_run_id:'bad'},{source_sha256:'bad'},{unexpected:true}])expect(()=>readLegacyEditorDraft({...draft(),...changed})).toThrow();
 expect(()=>readLegacyEditorDraft(draft(),{asset_version_id:id(2),source_sha256:'a'.repeat(64)})).toThrow();
});
it('refuses a save reply from another version or an unexpected CAS revision',async()=>{
 for(const changed of [{asset_version_id:id(2),revision:'1'},{revision:'2'}]){
  const request=vi.fn().mockResolvedValue({...draft(),...changed});const api=new OwnerAPI({request} as never,id(3));
  await expect(api.saveDraft(id(4),draft())).rejects.toThrow();
 }
});

it('passes save cancellation and refuses late adapter success after abort',async()=>{
 const controller=new AbortController();let resolve!:(value:unknown)=>void;const request=vi.fn((..._args:unknown[])=>new Promise<unknown>(done=>{resolve=done;}));const api=new OwnerAPI({request} as never,id(3));
 const pending=api.saveDraft(id(4),draft(),controller.signal);expect(request.mock.calls[0]?.[1]).toMatchObject({signal:controller.signal});
 controller.abort();resolve({...draft(),revision:'1'});await expect(pending).rejects.toMatchObject({name:'AbortError'});
});
it('returns an owned exact save reply and refuses pre-aborted save before transport',async()=>{
 const saved={...draft(),revision:'1'},request=vi.fn().mockResolvedValue(saved),api=new OwnerAPI({request} as never,id(3));
 const result=await api.saveDraft(id(4),draft());saved.params.modules.strength=.2;expect(result.params).toEqual({modules:{strength:1}});
 const controller=new AbortController();controller.abort();await expect(api.saveDraft(id(4),draft(),controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});

it.each(['preview','prompt','manual'] as const)('fences %s producer cancellation and freezes mutable evidence',async kind=>{
 const controller=new AbortController();let resolve!:(value:unknown)=>void;
 const request=vi.fn((..._args:unknown[])=>new Promise<unknown>(done=>{resolve=done;})),api=new OwnerAPI({request} as never,id(3));
 const input=draft(),points=[{x:.2,y:.3,label:'positive' as const}],strokes=[{kind:'stroke' as const,sequence:0,mode:'protect' as const,points:[{x:.2,y:.3}],radius:.1,hardness:1}];
 const pending=kind==='preview'?api.preview(input,'proxy',controller.signal):kind==='prompt'?api.promptMask(input,points,controller.signal):api.manualMask(input,strokes,[],controller.signal);
 input.params.modules.strength=.2;points[0].x=.9;strokes[0].points[0].x=.9;
 expect(request.mock.calls[0]?.[1]).toMatchObject({signal:controller.signal});
 const options=request.mock.calls[0]?.[1] as {body:Record<string,unknown>};
 if(kind==='preview')expect(options.body.params).toEqual({modules:{strength:1}});
 if(kind==='prompt')expect(options.body.points).toEqual([{x:.2,y:.3,label:'positive'}]);
 if(kind==='manual')expect(options.body.strokes).toEqual([{kind:'stroke',sequence:0,mode:'protect',points:[{x:.2,y:.3}],radius:.1,hardness:1}]);
 controller.abort();resolve({run_id:id(5),status:'queued'});await expect(pending).rejects.toMatchObject({name:'AbortError'});
 const before=request.mock.calls.length;
 await expect(kind==='preview'?api.preview(input,'proxy',controller.signal):kind==='prompt'?api.promptMask(input,points,controller.signal):api.manualMask(input,strokes,[],controller.signal)).rejects.toMatchObject({name:'AbortError'});
 expect(request).toHaveBeenCalledTimes(before);
});

it.each(['override','edits'] as const)('fences collection %s producer late success',async kind=>{
 const controller=new AbortController();let resolve!:(value:unknown)=>void;const request=vi.fn((..._args:unknown[])=>new Promise<unknown>(done=>{resolve=done;})),api=new OwnerAPI({request} as never,id(3));
 const invoke=()=>kind==='override'?api.createOverride(draft(),controller.signal):api.saveEdits({id:id(8),revision:'1',name:'Selection'} as never,draft(),id(9),id(10),undefined,controller.signal);
 const pending=invoke();expect(request.mock.calls[0]?.[1]).toMatchObject({signal:controller.signal});controller.abort();resolve({id:id(11),run_id:id(12)});await expect(pending).rejects.toMatchObject({name:'AbortError'});
 await expect(invoke()).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
it('does not start V2 creation or protection reads for an already cancelled editor action',async()=>{
 const controller=new AbortController();controller.abort();const request=vi.fn(),api=new OwnerAPI({request} as never,id(3));
 await expect(api.createV2Draft(id(4),{} as never,undefined,undefined,controller.signal)).rejects.toMatchObject({name:'AbortError'});
 await expect(api.bindProtectionToV2Draft(id(4),{} as never,controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).not.toHaveBeenCalled();
});
