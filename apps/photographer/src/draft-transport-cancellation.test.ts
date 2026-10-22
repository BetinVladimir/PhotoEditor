import {describe,it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const recipe=()=>({schema_version:3 as const,source:{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},base_style:ref(4),session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,renderer_version:'v2.2' as const,parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}});
const calls=[(api:OwnerAPI,s:AbortSignal)=>api.v2Document(id(9),s),(api:OwnerAPI,s:AbortSignal)=>api.saveV2Draft(id(1),'0',recipe(),null,'draft-key',s),(api:OwnerAPI,s:AbortSignal)=>api.bridgeEditorRecipe(id(1),{expected_revision:'2',recipe:recipe(),layers:null,editor_bridge:{revision:'1',document_id:id(9),params:{schema_version:1,modules:{}}}},'bridge-key',s)];
describe('Draft transport lifecycle',()=>{
 it.each(calls)('rejects pre-aborted scope without transport',async run=>{const request=vi.fn(),api=new OwnerAPI({request} as never,id(10)),controller=new AbortController();controller.abort();await expect(run(api,controller.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();});
 it.each(calls)('refuses ignored-transport late replies after cancellation',async run=>{let finish!:(value:unknown)=>void;const request=vi.fn(()=>new Promise(resolve=>{finish=resolve})),api=new OwnerAPI({request} as never,id(10)),controller=new AbortController();const pending=run(api,controller.signal);controller.abort();finish(recipe());await expect(pending).rejects.toThrow();expect(request).toHaveBeenCalledTimes(1);});
 it('rejects malformed recipe and bridge identity before any IO',async()=>{const request=vi.fn(),api=new OwnerAPI({request} as never,id(10));await expect(api.saveV2Draft(id(1),'0',{...recipe(),protected_mask:null} as never)).rejects.toThrow();await expect(api.bridgeEditorRecipe(id(1),{expected_revision:'2',recipe:recipe(),layers:null,editor_bridge:{revision:'1',document_id:'bad',params:{schema_version:1,modules:{}}}},'key')).rejects.toThrow();expect(request).not.toHaveBeenCalled();});
});
