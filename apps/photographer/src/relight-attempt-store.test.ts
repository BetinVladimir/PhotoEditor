import {expect,it} from 'vitest';
import {RelightAttemptStore,clearRelightAttempts} from './relight-attempt-store';
import type {RelightAttemptRecord} from './relight-attempt';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function setup(){const data=new Map<string,string>();return {data,storage:{getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v);},removeItem:async(k:string)=>{data.delete(k);},getAllKeys:async()=>[...data.keys()]}};}
function record():RelightAttemptRecord{const ref={artifact_id:id,sha256:'a'.repeat(64)};return {schema_version:1,project:id,key:'retry-key',ticket:null,body:{schema_version:1,snapshot_id:id,ordinal:0,subject_kind:'manual',params:{schema_version:1,base:{asset_version_id:id,source:ref,canvas_hash:'b'.repeat(64)},strength:.5,depth:ref,subject:ref,protected:ref,calibration:ref,lights:[{x:.5,y:.5,radius:.2,ev:.5}]}}};}
it('isolates user/tenant/project/version and refuses changed unknown request',async()=>{
 const {storage}=setup(),store=new RelightAttemptStore(storage,'https://relight.test',id,id,id,id,lock);
 await store.write(record());
 const changed=record();changed.body.params.strength=.8;await expect(store.write(changed)).rejects.toThrow('CONFLICT');
 const anotherUser=new RelightAttemptStore(storage,'https://relight.test',other,id,id,id,lock);expect(await anotherUser.read()).toBeNull();
 const wrongVersion=new RelightAttemptStore(storage,'https://relight.test',id,id,id,other,lock);await expect(wrongVersion.write(record())).rejects.toThrow('SCOPE');
});
it('retains unknown attempts and allows clearing only acknowledged matching key',async()=>{
 const {storage}=setup(),store=new RelightAttemptStore(storage,'https://relight-clear.test',id,id,id,id,lock);
 await store.write(record());await expect(store.clear('retry-key')).rejects.toThrow('CONFLICT');
 const accepted=record();accepted.ticket={run_id:id,workflow_id:other,state:'queued'};await store.write(accepted);
 await expect(store.write(record())).rejects.toThrow('CONFLICT');await expect(store.clear('wrong')).rejects.toThrow('CONFLICT');
 await store.clear('retry-key');expect(await store.read()).toBeNull();
});
it('logout removes only matching origin and fences existing instances',async()=>{
 const {storage}=setup(),origin='https://relight-logout.test',store=new RelightAttemptStore(storage,origin,id,id,id,id,lock),otherOrigin=new RelightAttemptStore(storage,'https://another.test',id,id,id,id,lock);
 await store.write(record());await otherOrigin.write(record());await clearRelightAttempts(storage,origin,lock);
 await expect(store.write(record())).rejects.toThrow('EXPIRED');expect(await otherOrigin.read()).not.toBeNull();
 const fresh=new RelightAttemptStore(storage,origin,id,id,id,id,lock);expect(await fresh.read()).toBeNull();
});
