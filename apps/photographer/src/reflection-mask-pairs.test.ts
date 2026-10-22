import {expect,it} from 'vitest';
import {collectReflectionMaskPairs,freezeReflectionMaskPairQuery,readReflectionMaskPairPage,type ReflectionMaskPairQuery} from './reflection-mask-pairs';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const query:ReflectionMaskPairQuery={snapshot_id:id(1),base:{asset_version_id:id(2),source:ref(3),canvas_hash:'b'.repeat(64)},after:null};
it('binds complete reflection mask producers and rejects donor-shaped responses',async()=>{
 const pair={mask:ref(4),protected:ref(5),target_pixel:ref(6),protected_pixel:ref(7)};
 expect(freezeReflectionMaskPairQuery(query)).toEqual(query);
 expect(await collectReflectionMaskPairs(async()=>({items:[pair],next_cursor:null}),query)).toEqual([pair]);
 expect(()=>readReflectionMaskPairPage({items:[{...pair,glasses_mask:ref(8)}],next_cursor:null},query)).toThrow();
 expect(()=>readReflectionMaskPairPage({items:[{...pair,protected_pixel:pair.target_pixel}],next_cursor:null},query)).toThrow();
 expect(()=>readReflectionMaskPairPage({items:[pair],next_cursor:pair.mask.artifact_id},query)).toThrow();
});
it('rejects UUIDs missing a canonical group instead of accepting shortened identifiers',()=>{
 const shortened='00000000-0000-4000-000000000001';
 expect(()=>freezeReflectionMaskPairQuery({...query,snapshot_id:shortened})).toThrow('REFLECTION_MASK_PAIR_QUERY_INVALID');
 const pair={mask:ref(4),protected:ref(5),target_pixel:ref(6),protected_pixel:ref(7)};
 expect(()=>readReflectionMaskPairPage({items:[{...pair,mask:{...pair.mask,artifact_id:shortened}}],next_cursor:null},query)).toThrow('REFLECTION_MASK_PAIR_PAGE_INVALID');
});
