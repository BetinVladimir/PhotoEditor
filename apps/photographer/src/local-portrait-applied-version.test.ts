import {expect,it} from 'vitest';
import {readLocalPortraitAppliedVersion} from './local-portrait-applied-version';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const source={artifact_id:id(4),sha256:'a'.repeat(64)};
const selection={schema_version:1 as const,review_id:id(1),decision_hash:'b'.repeat(64),route:'candidate' as const,image:source};
const result={asset_id:id(5),project_id:id(6),asset_version_id:id(7),review_id:id(1),decision_hash:selection.decision_hash,source};
it('owns an exact saved version response without sharing mutable refs',()=>{
 const saved=readLocalPortraitAppliedVersion(result,selection);
 expect(saved).toEqual(result);expect(saved).not.toBe(result);expect(saved.source).not.toBe(result.source);
});
it('accepts the selected original without silently changing its source',()=>{
 expect(readLocalPortraitAppliedVersion(result,{...selection,route:'base'})).toEqual(result);
});
it('rejects foreign decision, review, source and malformed version identifiers',()=>{
 for(const value of [{...result,review_id:id(20)},{...result,decision_hash:'c'.repeat(64)},{...result,source:{...source,artifact_id:id(21)}},{...result,asset_version_id:'bad'},{...result,extra:true},{...result,source:{...source,extra:true}}]){
  expect(()=>readLocalPortraitAppliedVersion(value,selection)).toThrow('LOCAL_PORTRAIT_APPLY_RESULT_INVALID');
 }
});
it('never materializes a skipped or missing selection',()=>{
 expect(()=>readLocalPortraitAppliedVersion(result,{...selection,route:'skip',image:null})).toThrow();
});
