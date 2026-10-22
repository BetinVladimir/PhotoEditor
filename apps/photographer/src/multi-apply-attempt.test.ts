import {perspectiveQualityTask} from './perspective-quality';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {MultiIntent,MultiOperation} from './multi-intent';
import {it,expect,vi} from 'vitest';
import type {MultiProposal} from './multi-proposal';
import type {MultiQuality} from './multi-quality';
import {MultiApplyAttempt,type MultiApplyRecord} from './multi-apply-attempt';
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
it('persists version before CAS and recovers lost CAS response',async()=>{
 const writes:MultiApplyRecord[]=[];
 const journal={write:async(r:MultiApplyRecord)=>{writes.push(structuredClone(r));}};
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,{collection:id(40),item:id(41),asset:id(42),revision:'1',expectedVersion:id(22)},journal);
 const result={asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)};
 const api={applyMulti:vi.fn().mockResolvedValue(result),reconcileCollectionVersion:vi.fn().mockResolvedValueOnce('unchanged').mockResolvedValueOnce('applied'),replaceCollectionVersion:vi.fn(async()=>{expect(writes.at(-1)?.applied).toEqual(result);throw new Error('lost response');})};
 expect(await attempt.resume(api)).toEqual(result);expect(attempt.snapshot.selected).toBe(true);
 const resumed=MultiApplyAttempt.restore(attempt.snapshot,journal);await resumed.resume(api);
 expect(api.applyMulti).toHaveBeenCalledTimes(1);expect(api.replaceCollectionVersion).toHaveBeenCalledTimes(1);
});
it('conflict never writes or changes the original revision',async()=>{
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,{collection:id(40),item:id(41),asset:id(42),revision:'1',expectedVersion:id(22)},{write:async()=>{}});
 const api={applyMulti:vi.fn().mockResolvedValue({asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)}),reconcileCollectionVersion:vi.fn().mockResolvedValue('conflict'),replaceCollectionVersion:vi.fn()};
 await expect(attempt.resume(api)).rejects.toThrow('CONFLICT');expect(api.replaceCollectionVersion).not.toHaveBeenCalled();expect(attempt.snapshot.selection?.revision).toBe('1');
});

it('rejects extra or missing durable envelope fields before restoring an operation',async()=>{
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,null,{write:async()=>{}});
 const record=attempt.snapshot;
 expect(()=>MultiApplyAttempt.restore({...record,unexpected:true} as unknown as MultiApplyRecord,{write:async()=>{}})).toThrow('MULTI_APPLY_JOURNAL_INVALID');
 const {selected:_,...missing}=record;
 expect(()=>MultiApplyAttempt.restore(missing as MultiApplyRecord,{write:async()=>{}})).toThrow('MULTI_APPLY_JOURNAL_INVALID');
});


it('keeps Apply state independent of journal adapter mutations',async()=>{
 const journal={write:async(record:MultiApplyRecord)=>{record.selected=true;if(record.applied)record.applied.asset_version_id=id(99);record.warnings=true;}};
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,null,journal);
 expect(attempt.snapshot.selected).toBe(false);expect(attempt.snapshot.warnings).toBe(false);
 const result={asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)};
 const api={applyMulti:vi.fn().mockResolvedValue(result),reconcileCollectionVersion:vi.fn(),replaceCollectionVersion:vi.fn()};
 expect(await attempt.resume(api)).toEqual(result);expect(attempt.snapshot.applied).toEqual(result);expect(attempt.snapshot.selected).toBe(false);
});
it.each([undefined,null,'unknown',true])('refuses malformed collection reconciliation %j',async state=>{
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,{collection:id(40),item:id(41),asset:id(42),revision:'1',expectedVersion:id(22)},{write:async()=>{}});
 const api={applyMulti:vi.fn().mockResolvedValue({asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)}),reconcileCollectionVersion:vi.fn().mockResolvedValue(state),replaceCollectionVersion:vi.fn()};
 await expect(attempt.resume(api)).rejects.toThrow('MULTI_APPLY_RECONCILIATION_INVALID');expect(attempt.snapshot.selected).toBe(false);expect(api.replaceCollectionVersion).not.toHaveBeenCalled();
});


it('keeps original collection CAS scope across reconciliation and ambiguous replacement',async()=>{
 const selected={collection:id(40),item:id(41),asset:id(42),revision:'1',expectedVersion:id(22)};
 const attempt=await MultiApplyAttempt.create(fixture(),intent(),false,selected,{write:async()=>{}});
 const expected={...selected,version:id(30)};let reconciles=0;
 const api={applyMulti:vi.fn().mockResolvedValue({asset_version_id:id(30),parent_asset_version_id:id(22),source:ref(6)}),
  reconcileCollectionVersion:vi.fn(async (selection:import('./collection-version').CollectionVersionSelection)=>{expect(selection).toEqual(expected);selection.item=id(99);return ++reconciles===1?'unchanged' as const:'applied' as const;}),
  replaceCollectionVersion:vi.fn(async (selection:import('./collection-version').CollectionVersionSelection)=>{expect(selection).toEqual(expected);selection.revision='99';selection.version=id(98);throw new Error('lost CAS reply');})};
 await attempt.resume(api);expect(attempt.snapshot.selected).toBe(true);expect(attempt.snapshot.selection).toEqual(selected);expect(reconciles).toBe(2);
});
