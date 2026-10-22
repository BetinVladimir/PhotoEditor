import {expect,it,vi} from 'vitest';
import {readProject,readProjectPage,readStylePage,readStyleProfile} from './catalog-read-response';
import {OwnerAPI} from './owner-api';
import {projectCollectionPages} from './project-collection-pages';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const common=()=>({id:id(1),tenant_id:id(2),name:'Catalog',description:'',revision:'9223372036854775807',created_at:'2026-10-17T10:00:00Z'});
const project=()=>({...common(),visibility:'restricted',download_allowed:false,can_edit:false});
const style=()=>({...common(),project_id:id(3),archived:false});
it('accepts actual detached project/style projections and preserves full revision strings',()=>{const p=project(),s=style();expect(readProject(p,id(2),id(1),true)).toEqual(p);expect(readStyleProfile(s,id(2),{project:id(3),id:id(1)})).toEqual(s);p.name='changed';expect(readProject(project(),id(2)).revision).toBe('9223372036854775807');});
it('refuses foreign tenant/id/project and malformed fields before publication',()=>{
 for(const p of [{...project(),tenant_id:id(9)},{...project(),id:'wrong'},{...project(),revision:'01'},{...project(),revision:'9223372036854775808'},{...project(),download_allowed:'yes'},{...project(),created_at:'1'},{...project(),name:{}}])expect(()=>readProject(p,id(2))).toThrow();
 expect(()=>readProject({...project(),can_edit:undefined},id(2),id(1),true)).toThrow();expect(()=>readStyleProfile(style(),id(2),{project:id(8)})).toThrow();expect(()=>readStyleProfile({...style(),archived:'false'},id(2))).toThrow();
});
it('refuses duplicate identities, cursor cycles and empty continuations on actual pages',()=>{for(const raw of [{items:[project(),project()],next_cursor:null},{items:[],next_cursor:'next'},{items:[project()],next_cursor:'same'},{items:[project()],next_cursor:[]},{items:Array.from({length:101},()=>project()),next_cursor:null}])expect(()=>readProjectPage(raw,id(2),'same')).toThrow();expect(()=>readStylePage({items:[style()],next_cursor:null},id(2),id(8))).toThrow();});
it('mounts strict OwnerAPI styles/detail/project reads with exact filters and late abort',async()=>{
 const request=vi.fn().mockResolvedValue({items:[style()],next_cursor:null}),api=new OwnerAPI({request} as never,id(2));expect((await api.styles(id(3))).items[0].project_id).toBe(id(3));expect(request.mock.calls[0][0]).toContain(`project_id=${id(3)}`);
 request.mockResolvedValue({...style(),id:id(8)});await expect(api.style(id(1))).rejects.toThrow();request.mockResolvedValue({items:[{...project(),tenant_id:id(8)}],next_cursor:null});await expect(api.projects()).rejects.toThrow();
 let resolve!:(v:unknown)=>void;request.mockImplementation(()=>new Promise(done=>{resolve=done}));const stop=new AbortController(),pending=api.styles(id(3),undefined,stop.signal);stop.abort();resolve({items:[style()],next_cursor:null});await expect(pending).rejects.toMatchObject({name:'AbortError'});
});
it('strictly validates project collection rows at the mounted tenant-bound call',async()=>{
 const collection={...common(),project_id:id(3),state:'draft',sort_mode:'manual'};
 expect(await projectCollectionPages(id(3),async()=>({items:[collection],next_cursor:null}),new AbortController().signal,id(2))).toEqual([collection]);
 await expect(projectCollectionPages(id(3),async()=>({items:[{...collection,tenant_id:id(8)}],next_cursor:null}),new AbortController().signal,id(2))).rejects.toThrow();
});
