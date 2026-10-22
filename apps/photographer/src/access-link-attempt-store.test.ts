import {expect,it} from 'vitest';
import {AccessLinkAttemptStore,clearAccessLinkAttempts,type AccessLinkAttempt} from './access-link-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const memory=()=>{const data=new Map<string,string>();return {data,getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v);},removeItem:async(k:string)=>{data.delete(k);},getAllKeys:async()=>[...data.keys()]};};
const lock=async<T,>(_origin:string,action:()=>Promise<T>)=>action();
const attempt=():AccessLinkAttempt=>({schema_version:1,key:id(5),request:{label:'Client',permissions:{view:true,download:false,upload:true},limits:{max_upload_files:10,max_upload_bytes_total:1000,max_file_bytes:1000,allowed_formats:['jpg']},moderation:'owner_review',allowed_presets:[]},ticket:null});
it('persists immutable intent, accepted identity only and refuses changed body or ticket downgrade',async()=>{
 const storage=memory(),store=new AccessLinkAttemptStore(storage,'https://links.example',id(1),id(2),id(3),lock),value=attempt();await store.write(value);value.request.label='Changed';expect((await store.read())!.request.label).toBe('Client');await expect(store.write(value)).rejects.toThrow();const accepted={...attempt(),ticket:{resource_id:id(6)}};await store.write(accepted);expect([...storage.data.values()].join('')).not.toContain('secret');await expect(store.write(attempt())).rejects.toThrow();await expect(store.clearAccepted(id(5),id(9))).rejects.toThrow();expect(await store.read()).toEqual(accepted);
});
it('logout invalidates old instances and prevents late resurrection',async()=>{
 const storage=memory(),origin='https://links-logout.example',old=new AccessLinkAttemptStore(storage,origin,id(1),id(2),id(3),lock);await old.write(attempt());await clearAccessLinkAttempts(storage,origin,lock);await expect(old.write(attempt())).rejects.toThrow();const fresh=new AccessLinkAttemptStore(storage,origin,id(1),id(2),id(3),lock);expect(await fresh.read()).toBeNull();
});
