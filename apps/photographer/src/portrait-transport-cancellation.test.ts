import {it,expect,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import type {GlareSourceIntent} from './glare-source-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const body=():GlareSourceIntent=>({schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,source_observation_id:id(3),params_proposal:{schema_version:1,mode:'generative',base:{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},donor:null,donor_faces:null,strength:.5,glasses_mask:ref(9),protected:ref(10),confirmation:ref(11),source_faces:ref(12),model_lock_sha:'d'.repeat(64),calibration:ref(14)}});
const keyed=['submitRelightAnalysis','submitRelight','submitGlareDonor','prepareGlareSource','submitGlareSource','submitOpenEyes','prepareOpenEyes','submitReflection','prepareReflection','prepareGlareDonor','prepareLocalPortrait','submitLocalPortrait'] as const;
const unkeyed=['prepareRelight','relightCandidates','openEyesSourceCandidates','openEyesDonorCandidates','openEyesMaskPairs','glareDonorCandidates','glareSourceCandidates','glareMaskPairs','reflectionMaskPairs','localPortraitMaskPairs'] as const;
it.each(keyed)('%s refuses a pre-cancelled scope before validation or transport',async method=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(20)),abort=new AbortController();abort.abort();
 await expect(api[method](id(21),null as never,'stable-key',abort.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).not.toHaveBeenCalled();
});
it.each(unkeyed)('%s refuses a pre-cancelled query before transport',async method=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(20)),abort=new AbortController();abort.abort();
 await expect(api[method](id(21),null as never,abort.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).not.toHaveBeenCalled();
});
it('does not expose a valid late queued receipt after cancellation and owns its submitted body',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn((_path:string,_options:{body:GlareSourceIntent})=>new Promise(done=>{resolve=done;})),api=new OwnerAPI({request} as never,id(20)),abort=new AbortController(),original=body();
 const pending=api.submitGlareSource(id(21),original,'stable-key',abort.signal);original.params_proposal.strength=1;
 expect(request.mock.calls[0][1].body.params_proposal.strength).toBe(.5);abort.abort();resolve({run_id:id(30),workflow_id:id(31),task_id:id(32),state:'queued'});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
it('validates the glare capability project before IO and fences a late boolean response',async()=>{
 let resolve!:(v:unknown)=>void;const request=vi.fn(()=>new Promise(done=>{resolve=done;})),api=new OwnerAPI({request} as never,id(20));
 await expect(api.glareSourceAvailable('../foreign')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 const abort=new AbortController(),pending=api.glareSourceAvailable(id(21),abort.signal);abort.abort();resolve({available:true});await expect(pending).rejects.toMatchObject({name:'AbortError'});
});
it('refuses malformed glare source submission scope or key before transport',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(20));
 await expect(api.submitGlareSource('../foreign',body(),'stable-key')).rejects.toThrow();
 await expect(api.submitGlareSource(id(21),body(),'bad key')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});

it('refuses malformed local portrait result path identities before transport',async()=>{
 const request=vi.fn(),api=new OwnerAPI({request} as never,id(20));
 await expect(api.discoverLocalPortraitResult('../foreign')).rejects.toThrow();
 await expect(api.localPortraitResult('../foreign',id(21))).rejects.toThrow();
 await expect(api.localPortraitResult(id(21),'bad-run')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});
