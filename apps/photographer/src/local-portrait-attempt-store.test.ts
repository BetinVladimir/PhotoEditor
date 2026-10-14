import {expect,it} from 'vitest';
import {LocalPortraitAttemptStore,clearLocalPortraitAttempts} from './local-portrait-attempt-store';
import type {LocalPortraitAttemptRecord} from './local-portrait-attempt';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function setup(){const data=new Map<string,string>();return {storage:{getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v);},removeItem:async(k:string)=>{data.delete(k);},getAllKeys:async()=>[...data.keys()]}};}
function record():LocalPortraitAttemptRecord{return {schema_version:1,project:id(20),key:'local-retry',ticket:null,body:{schema_version:1,snapshot_id:id(1),ordinal:0,operation:'portrait.hair',params:{schema_version:1,base:{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},strength:.5,mask:ref(4),protected:ref(5),hue_delta_degrees:0,saturation_factor:1,detail:.3},target_pixel:ref(6),protected_pixel:ref(7)}};}
it('keeps one immutable local portrait attempt until admission',async()=>{
 const {storage}=setup(),origin='https://local.test';
 const store=new LocalPortraitAttemptStore(storage,origin,id(30),id(31),id(20),id(2),lock);
 await store.write(record());
 const changed=record();changed.body.params.strength=.8;
 await expect(store.write(changed)).rejects.toThrow('CONFLICT');
 await expect(store.clear('local-retry')).rejects.toThrow('CONFLICT');
 const accepted=record();accepted.ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued'};
 await store.write(accepted);await store.clear('local-retry');expect(await store.read()).toBeNull();
});
it('fences logout while preserving another origin',async()=>{
 const {storage}=setup(),origin='https://local-logout.test';
 const store=new LocalPortraitAttemptStore(storage,origin,id(30),id(31),id(20),id(2),lock);
 const other=new LocalPortraitAttemptStore(storage,'https://other.test',id(30),id(31),id(20),id(2),lock);
 await store.write(record());await other.write(record());await clearLocalPortraitAttempts(storage,origin,lock);
 await expect(store.write(record())).rejects.toThrow('EXPIRED');
 expect(await other.read()).not.toBeNull();
});
