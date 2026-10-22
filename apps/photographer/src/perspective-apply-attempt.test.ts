import {it,expect,vi} from 'vitest';
import type {PerspectiveProposal} from './perspective-proposal';
import {perspectiveQualityTask,type PerspectiveQuality} from './perspective-quality';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {PerspectiveApplyAttempt,type PerspectiveApplyRecord} from './perspective-apply-attempt';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const proposal:PerspectiveProposal={schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};

function fixture():PerspectiveQuality{const v:PerspectiveQuality={proposal:structuredClone(proposal),quality_run_id:id(20),quality_task_id:perspectiveQualityTask(id(20)),quality_result_id:id(22),quality_report:ref(23),quality:{target:ref(8),target_kind:'working',stage:'master',threshold_profile_id:'master_v1',status:'pass',reference:null,reasons:[],technical:{below_zero_fraction:0,above_one_fraction:0,clipped_black_fraction:0,clipped_white_fraction:0,sharpness_laplacian:.01,seam_p99_delta:0,finite:true}}};v.quality_report.sha256=bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v.quality))));return v;}

it('persists version before CAS and recovers lost CAS response',async()=>{
 const writes:PerspectiveApplyRecord[]=[];
 const journal={write:async(r:PerspectiveApplyRecord)=>{writes.push(structuredClone(r));}};
 const attempt=await PerspectiveApplyAttempt.create(fixture(),false,{collection:id(40),item:id(41),asset:id(42),revision:'1',expectedVersion:id(6)},journal);
 const result={asset_version_id:id(30),parent_asset_version_id:id(6),source:ref(8)};
 const api={applyPerspective:vi.fn().mockResolvedValue(result),reconcileCollectionVersion:vi.fn().mockResolvedValueOnce('unchanged').mockResolvedValueOnce('applied'),replaceCollectionVersion:vi.fn(async()=>{expect(writes.at(-1)?.applied).toEqual(result);throw new Error('lost response');})};
 expect(await attempt.resume(api)).toEqual(result);expect(attempt.snapshot.selected).toBe(true);
 const resumed=PerspectiveApplyAttempt.restore(attempt.snapshot,journal);await resumed.resume(api);
 expect(api.applyPerspective).toHaveBeenCalledTimes(1);expect(api.replaceCollectionVersion).toHaveBeenCalledTimes(1);
});
it('conflict never writes or changes the original revision',async()=>{
 const attempt=await PerspectiveApplyAttempt.create(fixture(),false,{collection:id(40),item:id(41),asset:id(42),revision:'1',expectedVersion:id(6)},{write:async()=>{}});
 const api={applyPerspective:vi.fn().mockResolvedValue({asset_version_id:id(30),parent_asset_version_id:id(6),source:ref(8)}),reconcileCollectionVersion:vi.fn().mockResolvedValue('conflict'),replaceCollectionVersion:vi.fn()};
 await expect(attempt.resume(api)).rejects.toThrow('CONFLICT');expect(api.replaceCollectionVersion).not.toHaveBeenCalled();expect(attempt.snapshot.selection?.revision).toBe('1');
});
