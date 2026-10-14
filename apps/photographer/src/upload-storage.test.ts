import {expect,it,vi} from 'vitest';
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_key:string,action:()=>Promise<unknown>)=>action()}));
import {ScopedUploadStorage,clearUploadStorage} from './upload-storage';
import type {UploadRecord} from '@photoeditor/api-client';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(){const values=new Map<string,string>();return {values,io:{getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]}}}
const row=():UploadRecord=>({localId:id(9),tenant:id(2),projectId:id(3),collectionId:id(4),name:'frame.nef',size:1024,lastModified:0,sha256:'a'.repeat(64),parts:[],state:'paused'});
it('isolates owner, tenant, project, collection and origin and owns persisted rows',async()=>{
 const {io}=fixture(),origin='https://uploads-scope.test';const store=new ScopedUploadStorage(io,origin,id(1),id(2),id(3),id(4));
 const input=row();await store.save([input]);input.name='changed';expect((await store.load())[0].name).toBe('frame.nef');
 for(const args of [[origin,id(5),id(2),id(3),id(4)],[origin,id(1),id(6),id(3),id(4)],[origin,id(1),id(2),id(7),id(4)],[origin,id(1),id(2),id(3),id(8)],['https://other-upload.test',id(1),id(2),id(3),id(4)]])expect(await new ScopedUploadStorage(io,...args as [string,string,string,string,string]).load()).toEqual([]);
 expect(()=>store.save([{...row(),tenant:id(6)}])).toThrow();
});
it('never restores unattributed legacy rows and fences old instances after logout',async()=>{
 const {io,values}=fixture(),origin='https://uploads-logout.test';values.set('photoeditor.uploads.v1',JSON.stringify([row()]));
 const old=new ScopedUploadStorage(io,origin,id(1),id(2),id(3),id(4));expect(await old.load()).toEqual([]);await old.save([row()]);
 const other=new ScopedUploadStorage(io,'https://uploads-retained.test',id(1),id(2),id(3),id(4));await other.save([row()]);
 await clearUploadStorage(io,origin);await expect(old.load()).rejects.toThrow('UPLOAD_SCOPE_EXPIRED');
 await expect(old.save([row()])).rejects.toThrow('UPLOAD_SCOPE_EXPIRED');
 expect(values.has('photoeditor.uploads.v1')).toBe(false);expect(await other.load()).toHaveLength(1);
 expect(await new ScopedUploadStorage(io,origin,id(1),id(2),id(3),id(4)).load()).toEqual([]);
});
it('rejects foreign persisted rows before returning records',async()=>{
 const {io,values}=fixture();const store=new ScopedUploadStorage(io,'https://uploads-corrupt.test',id(1),id(2),id(3),id(4));
 values.set(store.key,JSON.stringify([{...row(),projectId:id(7)}]));await expect(store.load()).rejects.toThrow('UPLOAD_SCOPE_INVALID');
});
