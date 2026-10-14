import {expect,it} from 'vitest';
import {freezeLocalPortraitSelection,readLocalPortraitPrepared,readLocalPortraitTicket,verifyLocalPortraitRecoverySource,type LocalPortraitSelection} from './local-portrait-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const base={asset_version_id:id(1),source:ref(2),canvas_hash:'b'.repeat(64)};
const shared={schema_version:1 as const,snapshot_id:id(3),ordinal:0,target_pixel:ref(6),protected_pixel:ref(7)};
const hair:LocalPortraitSelection={...shared,operation:'portrait.hair',params:{schema_version:1,base,strength:.5,mask:ref(4),protected:ref(5),hue_delta_degrees:10,saturation_factor:1.1,detail:.3}};
const clothing:LocalPortraitSelection={...shared,operation:'portrait.clothing',params:{schema_version:1,base,strength:.5,mask:ref(4),protected:ref(5),hue_delta_degrees:10,saturation_factor:1.1,wrinkle_strength:.3}};
it('keeps hair and clothing selections unprivileged and exact',()=>{
 for(const selection of [hair,clothing]){
  expect(freezeLocalPortraitSelection(selection)).toEqual(selection);
  expect(readLocalPortraitPrepared(JSON.parse(JSON.stringify(selection)),selection)).toEqual(selection);
  expect(verifyLocalPortraitRecoverySource(selection,base)).toEqual(selection);
  expect(()=>verifyLocalPortraitRecoverySource(selection,{...base,source:ref(9)})).toThrow();
  expect(()=>freezeLocalPortraitSelection({...selection,runtime_sha:'c'.repeat(64)} as LocalPortraitSelection)).toThrow();
  expect(()=>readLocalPortraitPrepared({...selection,protected_pixel:ref(8)},selection)).toThrow();
 }
 expect(()=>freezeLocalPortraitSelection({...hair,params:{...hair.params,hue_delta_degrees:31}})).toThrow();
 expect(()=>freezeLocalPortraitSelection({...clothing,params:{...clothing.params,wrinkle_strength:1.1}})).toThrow();
 expect(()=>freezeLocalPortraitSelection({...hair,target_pixel:hair.params.mask})).toThrow();
});
it('accepts only a queued exact admission ticket',()=>{
 const ticket={run_id:id(10),workflow_id:id(11),task_id:id(12),state:'queued' as const};
 expect(readLocalPortraitTicket(ticket)).toEqual(ticket);
 expect(()=>readLocalPortraitTicket({...ticket,scope:'other'})).toThrow();
});
