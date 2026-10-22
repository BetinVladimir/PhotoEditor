import {expect,it} from 'vitest';
import {readLocalPortraitResult,readLocalPortraitAutoAppliedVersion} from './local-portrait-result';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const result={run_id:id(1),result_id:id(2),operation:'portrait.hair' as const,base:ref(3),candidate:ref(4),review_id:null};
const version={asset_id:id(5),project_id:id(6),asset_version_id:id(7),run_id:result.run_id,result_id:result.result_id,operation:result.operation,source:result.candidate};
it('owns the exact pass result and saved version',()=>{
 const owned=readLocalPortraitResult(result,result.run_id);
 expect(owned).toEqual(result);expect(owned.candidate).not.toBe(result.candidate);
 expect(readLocalPortraitAutoAppliedVersion(version,owned)).toEqual(version);
});
it('refuses reviewed or foreign pass results and malformed refs',()=>{
 for(const bad of [{...result,review_id:id(8)},{...result,run_id:id(9)},{...result,operation:'portrait.glare'},{...result,candidate:result.base},{...result,candidate:{...result.candidate,extra:true}},{...result,extra:true}])expect(()=>readLocalPortraitResult(bad,result.run_id)).toThrow();
});
it('refuses a saved version of a different operation, run, result or selected source',()=>{
 for(const bad of [{...version,operation:'portrait.clothing'},{...version,run_id:id(9)},{...version,result_id:id(10)},{...version,source:result.base},{...version,asset_version_id:'bad'},{...version,extra:true}])expect(()=>readLocalPortraitAutoAppliedVersion(bad,result)).toThrow();
});
