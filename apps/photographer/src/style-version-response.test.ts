import {expect,it,vi} from 'vitest';
import {readStyleVersionPage} from './style-version-response';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const row=()=>({id:id(1),style_id:id(2),version_no:1,config:{nested:{value:1}},config_hash:'a'.repeat(64),schema_version:'2.0.0',renderer_version:'photo_renderer_v2',approval_status:'accepted',metrics:{confidence:0.9},created_at:'2026-10-17T12:00:00Z'});
it('reads actual immutable style versions without inventing tenant or revision fields',()=>{const v=row(),out=readStyleVersionPage({items:[v],next_cursor:null},id(2));v.config.nested.value=9;expect(out.items[0].config).toEqual({nested:{value:1}});expect(readStyleVersionPage({items:[{...row(),approval_status:'needs_review',version_no:2147483647}],next_cursor:null},id(2)).items[0].version_no).toBe(2147483647);});
it('refuses substituted styles and malformed renderer/hash/config/pagination before Apply consumers',()=>{
 for(const patch of [{style_id:id(3)},{id:'bad'},{version_no:2147483648},{version_no:1.5},{schema_version:'future'},{renderer_version:'wrong'},{approval_status:'ready'},{config:[]},{metrics:null},{config_hash:'A'.repeat(64)},{training_run_id:null},{source_file_sha256:'bad'}])expect(()=>readStyleVersionPage({items:[{...row(),...patch}],next_cursor:null},id(2))).toThrow();
 for(const page of [{items:[row(),row()],next_cursor:null},{items:[],next_cursor:'next'},{items:[row()],next_cursor:'same'},{items:[{...row(),version_no:2},{...row(),id:id(3)}],next_cursor:null}])expect(()=>readStyleVersionPage(page,id(2),'same')).toThrow();
});
it('mounts style-scoped decoding and pre/post cancellation in OwnerAPI',async()=>{
 const request=vi.fn().mockResolvedValue({items:[{...row(),style_id:id(3)}],next_cursor:null});const api=new OwnerAPI({request} as never,id(4));await expect(api.styleVersions(id(2))).rejects.toThrow();
 const stopped=new AbortController();stopped.abort();request.mockClear();await expect(api.styleVersions(id(2),undefined,stopped.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).not.toHaveBeenCalled();
 let resolve!:(v:unknown)=>void;request.mockImplementation(()=>new Promise(done=>{resolve=done}));const controller=new AbortController(),pending=api.styleVersions(id(2),undefined,controller.signal);controller.abort();resolve({items:[row()],next_cursor:null});await expect(pending).rejects.toMatchObject({name:'AbortError'});
});
