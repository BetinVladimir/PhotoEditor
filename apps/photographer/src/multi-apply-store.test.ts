import {perspectiveQualityTask} from './perspective-quality';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {MultiIntent,MultiOperation} from './multi-intent';
import {it,expect} from 'vitest';
import type {MultiProposal} from './multi-proposal';
import type {MultiQuality} from './multi-quality';
import {MultiApplyAttempt} from './multi-apply-attempt';
import {MultiApplyStore,clearMultiApplyAttempts,listMultiApplyAttempts} from './multi-apply-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
function scopeFixture(op:MultiOperation){
 const frames=[{asset_version_id:id(20),source:ref(21),canvas_hash:'b'.repeat(64)},{asset_version_id:id(22),source:ref(23),canvas_hash:'c'.repeat(64)}];
 const common={schema_version:1 as const,frames,reference_ordinal:1,max_pixels:10000,calibration:ref(24)};
 const intent:MultiIntent={schema_version:1,snapshot_id:id(25),params:op==='optical.panorama'?{...common,projection:'planar'}:{...common,motion_policy:'review'}};
 const proposal:MultiProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),operation:op,result_id:id(4),result:ref(5),sources:structuredClone(frames),reference_ordinal:1,working_image:ref(6),changed_mask:ref(7),report:ref(8),registrations:[ref(9),ref(10)],canvas:{width:100,height:100,orientation:1,space_id:'multi:'+'d'.repeat(32),source_sha256:'e'.repeat(64)},status:'review',requires_review:true,reason:op.slice(8).toUpperCase()+'_REVIEW;motion_fraction=0.125;REGISTRATION_INCONSISTENT'};
 return {intent,proposal};
}

function quality(proposal:MultiProposal):MultiQuality{const value:MultiQuality={proposal:structuredClone(proposal),quality_run_id:id(40),quality_task_id:perspectiveQualityTask(id(40)),quality_result_id:id(42),quality_report:ref(43),quality:{target:proposal.working_image,target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};seal(value);return value;}
function seal(v:MultiQuality){v.quality_report.sha256=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v.quality))));}

function fixture(){return quality(scopeFixture("optical.panorama").proposal);}
const intent=()=>scopeFixture("optical.panorama").intent;
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v);},removeItem:async(k:string)=>{values.delete(k);},getAllKeys:async()=>[...values.keys()]};}
const lock=async<T,>(_origin:string,action:()=>Promise<T>)=>action();
it('owns journal scope and refuses substituted pending decisions',async()=>{
 const disk=storage(),store=new MultiApplyStore(disk,'https://studio.test',id(50),id(1),id(2),id(3),lock);
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,null,store);
 expect((await store.read())?.review.quality_result_id).toBe(id(42));
 const bad=attempt.snapshot;bad.warnings=true;await expect(store.write(bad)).rejects.toThrow('CONFLICT');
 await expect(store.clear(id(42))).rejects.toThrow('CONFLICT');
 const other=new MultiApplyStore(disk,'https://studio.test',id(51),id(1),id(2),id(3),lock);expect(await other.read()).toBeNull();
});
it('logout fences old instances and removes persisted attempts',async()=>{
 const disk=storage(),store=new MultiApplyStore(disk,'https://studio.test',id(50),id(1),id(2),id(3),lock);
 await MultiApplyAttempt.create(fixture(),intent(),false,null,store);
 await clearMultiApplyAttempts(disk,'https://studio.test',lock);
 await expect(store.read()).rejects.toThrow('EXPIRED');
 const fresh=new MultiApplyStore(disk,'https://studio.test',id(50),id(1),id(2),id(3),lock);expect(await fresh.read()).toBeNull();
});
it('serializes separate restored controllers and refreshes persisted version before dispatch',async()=>{
 const disk=storage(),origin='https://concurrent.studio.test';
 const firstStore=new MultiApplyStore(disk,origin,id(50),id(1),id(2),id(3),lock);
 const first=await MultiApplyAttempt.create(fixture(),intent(),false,{collection:id(60),item:id(61),asset:id(62),revision:'7',expectedVersion:id(22)},firstStore);
 const secondStore=new MultiApplyStore(disk,origin,id(50),id(1),id(2),id(3),lock);
 const second=MultiApplyAttempt.restore((await secondStore.read())!,secondStore);
 let release!:(value:{asset_version_id:string;parent_asset_version_id:string;source:ReturnType<typeof ref>})=>void,entered!:()=>void;
 const started=new Promise<void>(resolve=>{entered=resolve;});let applies=0,replaces=0,reconciles=0;
 const result={asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)};
 const api={applyMulti:async()=>{applies++;entered();return new Promise<typeof result>(resolve=>{release=resolve;});},reconcileCollectionVersion:async()=>{reconciles++;return 'unchanged' as const;},replaceCollectionVersion:async()=>{replaces++;}};
 const one=first.resume(api);await started;const two=second.resume(api);release(result);
 expect(await Promise.all([one,two])).toEqual([result,result]);
 expect(applies).toBe(1);expect(replaces).toBe(1);expect(reconciles).toBe(1);expect(second.snapshot.selected).toBe(true);
});
it('logout fences an in-flight Apply before it can persist or select a collection version',async()=>{
 const disk=storage(),origin='https://logout-inflight.studio.test';
 const store=new MultiApplyStore(disk,origin,id(50),id(1),id(2),id(3),lock);
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,{collection:id(60),item:id(61),asset:id(62),revision:'7',expectedVersion:id(22)},store);
 let release!:(value:{asset_version_id:string;parent_asset_version_id:string;source:ReturnType<typeof ref>})=>void,entered!:()=>void;
 const started=new Promise<void>(resolve=>{entered=resolve;});let reconciles=0,replaces=0;
 const result={asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)};
 const api={applyMulti:async()=>{entered();return new Promise<typeof result>(resolve=>{release=resolve;});},reconcileCollectionVersion:async()=>{reconciles++;return 'unchanged' as const;},replaceCollectionVersion:async()=>{replaces++;}};
 const pending=attempt.resume(api);const refusal=expect(pending).rejects.toThrow('EXPIRED');await started;
 await clearMultiApplyAttempts(disk,origin,lock);release(result);await refusal;
 expect(reconciles).toBe(0);expect(replaces).toBe(0);expect(await disk.getItem(store.key)).toBeNull();
});

it('discovers collection attempts without a preparation journal and isolates users/collections',async()=>{
 const disk=storage(),origin='https://discover.studio.test';
 const store=new MultiApplyStore(disk,origin,id(50),id(1),id(2),id(3),lock);
 await MultiApplyAttempt.create(fixture(),intent(),false,{collection:id(60),item:id(61),asset:id(62),revision:'7',expectedVersion:id(22)},store);
 const scope={origin,user:id(50),tenant:id(1),project:id(2),collection:id(60)};
 expect((await listMultiApplyAttempts(disk,scope,lock)).records).toHaveLength(1);
 expect((await listMultiApplyAttempts(disk,{...scope,user:id(51)},lock)).records).toHaveLength(0);
 expect((await listMultiApplyAttempts(disk,{...scope,collection:id(63)},lock)).records).toHaveLength(0);
 await disk.setItem(store.key,'{broken');
 await expect(listMultiApplyAttempts(disk,scope,lock)).rejects.toThrow();
});
it('paginates owned records deterministically without dropping the last attempt',async()=>{
 const disk=storage(),origin='https://pages.studio.test',scope={origin,user:id(50),tenant:id(1),project:id(2),collection:id(60)};
 for(let n=100;n<121;n++){
  const review=fixture();review.proposal.producer_task_id=id(n);
  const store=new MultiApplyStore(disk,origin,id(50),id(1),id(2),id(n),lock);
  await MultiApplyAttempt.create(review,intent(),false,{collection:id(60),item:id(61),asset:id(62),revision:'7',expectedVersion:id(22)},store);
 }
 const first=await listMultiApplyAttempts(disk,scope,lock);expect(first.records).toHaveLength(20);expect(first.nextCursor).toBe(id(119));
 const last=await listMultiApplyAttempts(disk,{...scope,cursor:first.nextCursor!},lock);expect(last.records).toHaveLength(1);expect(last.records[0].review.proposal.producer_task_id).toBe(id(120));expect(last.nextCursor).toBeNull();
});
