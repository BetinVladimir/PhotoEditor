// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import type {StoredSemanticAttempt} from './semantic-attempt-store';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222',origin='https://lock.test';
const value=():StoredSemanticAttempt=>({schema_version:1,snapshotKey:id,proposalKey:other,collection:{id,revision:'1'},mask:{artifact_id:id,sha256:'a'.repeat(64)},kind:'local_exposure',strength:.5,ev:0,prompt:null,binding:null,snapshot:null,body:null,ticket:null});
afterEach(()=>{Reflect.deleteProperty(navigator,'locks');vi.resetModules()});
async function fixture(){
 const data=new Map<string,string>();const storage={getItem:vi.fn(async(k:string)=>{await Promise.resolve();return data.get(k)??null}),setItem:vi.fn(async(k:string,v:string)=>{await Promise.resolve();data.set(k,v)}),removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};
 let pending:Promise<unknown>=Promise.resolve();
 const request=vi.fn((_name:string,options:{mode:string},action:()=>Promise<unknown>)=>{expect(options.mode).toBe('exclusive');const result=pending.catch(()=>{}).then(action);pending=result;return result});
 Object.defineProperty(navigator,'locks',{configurable:true,value:{request}});
 // Separate module instances model independent tab queues and epoch maps.
 vi.resetModules();const first=await import('./semantic-attempt-store');vi.resetModules();const second=await import('./semantic-attempt-store');
 const a=new first.SemanticAttemptStore(storage,origin,id,id,id,id,id),b=new second.SemanticAttemptStore(storage,origin,id,id,id,id,id);
 return{a,b,first,storage,data,request};
}
it('serializes two independent tab queues so only one original request wins',async()=>{
 const f=await fixture();await Promise.all([f.a.read(),f.b.read()]);
 const results=await Promise.allSettled([f.a.write(value()),f.b.write({...value(),proposalKey:crypto.randomUUID()})]);
 expect(results.filter(x=>x.status==='fulfilled')).toHaveLength(1);expect(results.filter(x=>x.status==='rejected')).toHaveLength(1);
 expect(await f.a.read()).toEqual(value());expect(f.request).toHaveBeenCalledWith('photo.semantic-journal:'+origin,{mode:'exclusive'},expect.any(Function));
});
it('fences a previously opened tab after logout in another module instance',async()=>{
 const f=await fixture();await f.b.read();await f.a.write(value());await f.first.clearSemanticAttempts(f.storage,origin);
 await expect(f.b.write(value())).rejects.toThrow('EXPIRED');await expect(f.b.read()).rejects.toThrow('EXPIRED');expect(f.data.has(f.a.key)).toBe(false);
});
it('refuses browser storage IO without Web Locks instead of using a racy fallback',async()=>{
 const f=await fixture();Reflect.deleteProperty(navigator,'locks');await expect(f.a.write(value())).rejects.toThrow('LOCK_UNAVAILABLE');expect(f.storage.getItem).not.toHaveBeenCalled();expect(f.storage.setItem).not.toHaveBeenCalled();
});
