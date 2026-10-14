import {expect,it} from 'vitest';
import {OpenEyesAttemptStore,clearOpenEyesAttempts} from './open-eyes-attempt-store';
import type {OpenEyesAttemptRecord} from './open-eyes-attempt';

const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function setup(){const data=new Map<string,string>();return {storage:{getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v);},removeItem:async(k:string)=>{data.delete(k);},getAllKeys:async()=>[...data.keys()]}};}
function record():OpenEyesAttemptRecord{return {schema_version:1,project:id(20),key:'eyes-retry',ticket:null,body:{schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,
 source_observation_id:id(3),donor_observation_id:id(4),params_proposal:{schema_version:1,
 base:{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},
 donor:{asset_version_id:id(7),source:ref(8),canvas_hash:'c'.repeat(64)},strength:.5,
 source_face_id:id(16),donor_face_id:id(17),eyes_mask:ref(9),protected:ref(10),confirmation:ref(11),source_faces:ref(12),
 donor_faces:ref(13)},target_pixel:ref(14),protected_pixel:ref(15)}};}

it('retains ambiguous open eyes attempt and clears only acknowledged original key',async()=>{
 const {storage}=setup(),origin='https://eyes.test';
 const store=new OpenEyesAttemptStore(storage,origin,id(30),id(31),id(20),id(5),lock);
 await store.write(record());
 const changed=record();changed.body.params_proposal.strength=.8;
 await expect(store.write(changed)).rejects.toThrow('CONFLICT');
 await expect(store.clear('eyes-retry')).rejects.toThrow('CONFLICT');
 const accepted=record();accepted.ticket={run_id:id(21),workflow_id:id(22),task_id:id(23),state:'queued'};
 await store.write(accepted);
 await expect(store.write(record())).rejects.toThrow('CONFLICT');
 await store.clear('eyes-retry');expect(await store.read()).toBeNull();
});

it('fences logout and preserves another origin',async()=>{
 const {storage}=setup(),origin='https://eyes-logout.test';
 const store=new OpenEyesAttemptStore(storage,origin,id(30),id(31),id(20),id(5),lock);
 const other=new OpenEyesAttemptStore(storage,'https://other.test',id(30),id(31),id(20),id(5),lock);
 await store.write(record());await other.write(record());await clearOpenEyesAttempts(storage,origin,lock);
 await expect(store.write(record())).rejects.toThrow('EXPIRED');
 expect(await other.read()).not.toBeNull();
});
