import {expect,it} from 'vitest';
import {collectGlareDonorCandidates,collectGlareSourceCandidates,collectGlareMaskPairs,freezeGlareDonorCandidateQuery,freezeGlareSourceCandidateQuery,freezeGlareMaskPairQuery,readGlareDonorCandidatePage,readGlareSourceCandidatePage,readGlareMaskPairPage,verifyGlareDonorCandidateBinding,type GlareDonorCandidateQuery,type GlareSourceCandidateQuery,type GlareMaskPairQuery} from './glare-donor-candidates';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const query:GlareDonorCandidateQuery={shoot_session_id:id(1),snapshot_id:id(2),base:{asset_version_id:id(3),source:ref(4),canvas_hash:'b'.repeat(64)},source_observation_id:id(5),after:null};
it('rejects authority fields and duplicate donor observations',async()=>{
 expect(freezeGlareDonorCandidateQuery(query)).toEqual(query);
 expect(()=>freezeGlareDonorCandidateQuery({...query,qualification:{}} as GlareDonorCandidateQuery)).toThrow();
 const item={observation_id:id(6),asset_id:id(7),asset_version_id:id(8),source:ref(9),donor_faces:ref(10)};
 expect(()=>readGlareDonorCandidatePage({source_faces:ref(11),items:[item,item],next_cursor:null},query)).toThrow();
 const page={source_faces:ref(11),items:[item],next_cursor:null};
 expect((await collectGlareDonorCandidates(async()=>page,query)).items).toEqual([item]);
 expect(verifyGlareDonorCandidateBinding(item,{asset_version_id:item.asset_version_id,source:item.source,canvas_hash:'c'.repeat(64)}).asset_version_id).toBe(item.asset_version_id);
 expect(()=>verifyGlareDonorCandidateBinding(item,{asset_version_id:item.asset_version_id,source:ref(99),canvas_hash:'c'.repeat(64)})).toThrow();
});
it('pages confirmed source observations without accepting extra authority fields',async()=>{
 const source:GlareSourceCandidateQuery={shoot_session_id:query.shoot_session_id,snapshot_id:query.snapshot_id,base:query.base,after:null};
 expect(freezeGlareSourceCandidateQuery(source)).toEqual(source);
 expect(()=>freezeGlareSourceCandidateQuery({...source,consent_id:id(12)} as GlareSourceCandidateQuery)).toThrow();
 const item={observation_id:id(13),source_faces:ref(14)};
 expect(await collectGlareSourceCandidates(async()=>({items:[item],next_cursor:null}),source)).toEqual([item]);
});
it('keeps target, guard and physical pixels in one discovered producer pair',async()=>{
 const mask:GlareMaskPairQuery={snapshot_id:query.snapshot_id,base:query.base,after:null};
 expect(freezeGlareMaskPairQuery(mask)).toEqual(mask);
 const pair={glasses_mask:ref(20),protected:ref(21),target_pixel:ref(22),protected_pixel:ref(23)};
 expect(await collectGlareMaskPairs(async()=>({items:[pair],next_cursor:null}),mask)).toEqual([pair]);
 await expect(collectGlareMaskPairs(async()=>({items:[{...pair,protected_pixel:pair.target_pixel}],next_cursor:null}),mask)).rejects.toThrow();
});
it('accepts continuation only after a full page at its last item',()=>{
 const source:GlareSourceCandidateQuery={shoot_session_id:query.shoot_session_id,snapshot_id:query.snapshot_id,base:query.base,after:null};
 const mask:GlareMaskPairQuery={snapshot_id:query.snapshot_id,base:query.base,after:null};
 const sources=Array.from({length:100},(_,index)=>({observation_id:id(1000+index),source_faces:ref(2000+index)}));
 const pairs=Array.from({length:100},(_,index)=>({glasses_mask:ref(3000+index),protected:ref(4000+index),target_pixel:ref(5000+index),protected_pixel:ref(6000+index)}));
 const donors=Array.from({length:100},(_,index)=>({observation_id:id(7000+index),asset_id:id(8000+index),asset_version_id:id(9000+index),source:ref(10000+index),donor_faces:ref(11000+index)}));
 expect(readGlareSourceCandidatePage({items:sources,next_cursor:sources[99].observation_id},source).items).toHaveLength(100);
 expect(readGlareMaskPairPage({items:pairs,next_cursor:pairs[99].glasses_mask.artifact_id},mask).items).toHaveLength(100);
 expect(readGlareDonorCandidatePage({source_faces:ref(12),items:donors,next_cursor:donors[99].observation_id},query).items).toHaveLength(100);
 expect(()=>readGlareSourceCandidatePage({items:sources.slice(0,99),next_cursor:sources[98].observation_id},source)).toThrow();
 expect(()=>readGlareMaskPairPage({items:pairs,next_cursor:id(9999)},mask)).toThrow();
 expect(()=>readGlareDonorCandidatePage({source_faces:ref(12),items:donors,next_cursor:donors[98].observation_id},query)).toThrow();
});
