import {expect,it} from 'vitest';
import {verifyNegativeRecoverySource,type NegativeIntent} from './negative-intent';
import {verifyPerspectiveRecoverySource,type PerspectiveIntent} from './perspective-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const base={asset_version_id:id(1),source:{artifact_id:id(2),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)};
const negative:NegativeIntent={schema_version:1,snapshot_id:id(3),params:{schema_version:1,base,strength:1,film_base_rgb:[1,1,1],calibration:{artifact_id:id(4),sha256:'c'.repeat(64)},exposure_ev:0}};
const perspective:PerspectiveIntent={schema_version:1,snapshot_id:id(3),params:{schema_version:1,base,strength:1,mode:'manual',corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}};
for(const [name,verify] of [
 ['negative',(live:typeof base)=>verifyNegativeRecoverySource(negative,live)],
 ['perspective',(live:typeof base)=>verifyPerspectiveRecoverySource(perspective,live)],
] as const){
 it(`${name} returns an owned intent for an unchanged source`,()=>{
  const result=verify(base);expect(result.params.base).toEqual(base);expect(result.params.base).not.toBe(base);
 });
 it.each([
  {...base,asset_version_id:id(9)},
  {...base,source:{...base.source,artifact_id:id(9)}},
  {...base,source:{...base.source,sha256:'d'.repeat(64)}},
  {...base,canvas_hash:'d'.repeat(64)},
 ])(`${name} refuses changed source pins`,live=>{expect(()=>verify(live)).toThrow('RECOVERY_SOURCE_CHANGED');});
}
