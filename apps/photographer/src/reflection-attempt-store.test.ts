import {expect,it} from 'vitest';
import {ReflectionAttemptStore,clearReflectionAttempts} from './reflection-attempt-store';
import type {ReflectionAttemptRecord} from './reflection-attempt';

const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function setup(){const data=new Map<string,string>();return {storage:{getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v);},removeItem:async(k:string)=>{data.delete(k);},getAllKeys:async()=>[...data.keys()]}};}
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=()=>({schema_version:1 as const,snapshot_id:id(1),ordinal:0,params_proposal:{schema_version:1 as const,base:{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},strength:.5,mask:ref(4),protected:ref(5),model_lock_sha:'c'.repeat(64),calibration:ref(8)},target_pixel:ref(6),protected_pixel:ref(7)});
function record():ReflectionAttemptRecord{return {schema_version:1,project:id(20),key:'reflection-retry',ticket:null,body:body()};}

it('retains ambiguous reflection attempt and clears only accepted key',async()=>{
 const {storage}=setup(),origin='https://reflection.test';
 const store=new ReflectionAttemptStore(storage,origin,id(30),id(31),id(20),id(2),lock);
 await store.write(record());
 const changed=record();changed.body.params_proposal.strength=.8;
 await expect(store.write(changed)).rejects.toThrow('CONFLICT');
 await expect(store.clear('reflection-retry')).rejects.toThrow('CONFLICT');
 const accepted=record();accepted.ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued'};
 await store.write(accepted);
 await expect(store.write(record())).rejects.toThrow('CONFLICT');
 await store.clear('reflection-retry');expect(await store.read()).toBeNull();
});

it('fences logout and preserves another origin',async()=>{
 const {storage}=setup(),origin='https://reflection-logout.test';
 const store=new ReflectionAttemptStore(storage,origin,id(30),id(31),id(20),id(2),lock);
 const other=new ReflectionAttemptStore(storage,'https://other.test',id(30),id(31),id(20),id(2),lock);
 await store.write(record());await other.write(record());await clearReflectionAttempts(storage,origin,lock);
 await expect(store.write(record())).rejects.toThrow('EXPIRED');
 expect(await other.read()).not.toBeNull();
});
