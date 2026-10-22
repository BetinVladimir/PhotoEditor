import {expect,it,vi} from 'vitest';
import {readV2DraftResource,readV2DraftHistory} from './v2-draft-dto';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const stream=()=>({id:id(1),revision:'9007199254740993',document:{artifact_id:id(2),sha256:'a'.repeat(64)},data:{resource_type:'v2_document_streams',id:id(1),tenant_id:id(3),project_id:id(4),created_at:'2026-10-17T00:00:00Z',revision:'9007199254740993',updated_at:'2026-10-17T00:00:00Z',asset_version_id:id(5),stream_type:'draft',current_document_id:id(6),approved_document_id:null}});
it('keeps actual draft resource decimal revisions and nullable approval owned',()=>{
 const value=stream(),result=readV2DraftResource(value,id(3));value.document.sha256='b'.repeat(64);expect(result.document.sha256).toBe('a'.repeat(64));expect(result.revision).toBe('9007199254740993');
 for(const data of [{...stream().data,tenant_id:id(9)},{...stream().data,revision:'1'},{...stream().data,stream_type:'shot_list'},{...stream().data,current_document_id:null},{...stream().data,approved_document_id:'bad'},{...stream().data,unexpected:true}])expect(()=>readV2DraftResource({...stream(),data},id(3))).toThrow('V2_DRAFT_RESPONSE_INVALID');
});
it('rejects a foreign current-draft projection in actual OwnerAPI transport',async()=>{
 const request=vi.fn().mockResolvedValue(stream()),api=new OwnerAPI({request} as never,id(9));await expect(api.v2Draft(id(8))).rejects.toThrow('V2_DRAFT_RESPONSE_INVALID');
});

it('decodes immutable history document metadata without fabricating row revision',()=>{
 const row=(version:string,n:number)=>({id:id(n),revision:'1',document:{artifact_id:id(20+n),sha256:'a'.repeat(64)},data:{resource_type:'v2_documents',id:id(n),tenant_id:id(3),project_id:id(4),created_at:'2026-10-17T00:00:00Z',stream_id:id(1),version_no:version,document_type:'EditingRecipeV3',artifact_id:id(20+n),sha256:'a'.repeat(64)}});
 expect(readV2DraftHistory({items:[row('9007199254740993',7),row('2',8)],next_cursor:null},id(3)).items).toHaveLength(2);
 for(const page of [{items:[row('1',7),row('2',8)],next_cursor:null},{items:[row('1',7),row('1',7)],next_cursor:null},{items:[],next_cursor:'continuation'},{items:[{...row('1',7),revision:'2'}],next_cursor:null},{items:[row('1',7)],next_cursor:null,revision:'1'}])expect(()=>readV2DraftHistory(page,id(3))).toThrow('V2_HISTORY_RESPONSE_INVALID');
});

it.each(['save','bridge'] as const)('decodes %s reply against the frozen version and rejects late cancellation',async kind=>{
 const recipe={schema_version:3,source:{asset_version_id:id(5),source:{artifact_id:id(7),sha256:'b'.repeat(64)},canvas_hash:'c'.repeat(64)},base_style:{artifact_id:id(10),sha256:'a'.repeat(64)},session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,renderer_version:'v2.2',parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}} as never;
 const invoke=(api:OwnerAPI,signal?:AbortSignal)=>kind==='save'?api.saveV2Draft(id(8),'4',recipe,null,'same-key',signal):api.bridgeEditorRecipe(id(8),{expected_revision:'4',recipe,layers:null,editor_bridge:{revision:'1',document_id:id(6),params:{schema_version:1,modules:{}}}},'same-key',signal);
 for(const changed of [{...stream(),data:{...stream().data,asset_version_id:id(9)}},{...stream(),data:{...stream().data,tenant_id:id(9)}}]){
  const request=vi.fn().mockResolvedValue(changed);await expect(invoke(new OwnerAPI({request} as never,id(3)))).rejects.toThrow();
 }
 const valid=stream(),request=vi.fn().mockResolvedValue(valid),api=new OwnerAPI({request} as never,id(3));const saved=await invoke(api);valid.document.sha256='d'.repeat(64);expect(saved.document.sha256).toBe('a'.repeat(64));
 const controller=new AbortController();let finish!:(value:unknown)=>void;request.mockImplementationOnce(()=>new Promise(done=>{finish=done;}));const pending=invoke(api,controller.signal);controller.abort();finish(stream());await expect(pending).rejects.toMatchObject({name:'AbortError'});
 const count=request.mock.calls.length;await expect(invoke(api,controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(count);
});

it.each(['2026-10-17T00:00:00.1234567890Z','2026-10-17T00:00:00','1'])('rejects non-Go wire timestamps %s',value=>{expect(()=>readV2DraftResource({...stream(),data:{...stream().data,created_at:value}},id(3))).toThrow('V2_DRAFT_RESPONSE_INVALID');});
