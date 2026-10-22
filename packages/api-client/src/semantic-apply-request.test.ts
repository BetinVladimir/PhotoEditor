import {describe,it,expect} from 'vitest';
import {decodeSemanticApplyRequest} from './semantic-apply';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const request=()=>({expected_revision:'1',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id(1),decision:'approve',preview:{render:{artifact_id:id(2),sha256:'b'.repeat(64)},evidence:{artifact_id:id(3),sha256:'c'.repeat(64)}}}]});
describe('persisted semantic Apply request',()=>{
 it('owns the exact selected preview refs',()=>{const original=request(),owned=decodeSemanticApplyRequest(original);original.target_decisions[0].preview.render.sha256='d'.repeat(64);expect(owned.target_decisions[0].preview?.render.sha256).toBe('b'.repeat(64));});
 it('rejects unknown nested fields and aliased evidence',()=>{const value=request();expect(()=>decodeSemanticApplyRequest({...value,extra:true})).toThrow();value.target_decisions[0].preview.evidence.artifact_id=id(2);expect(()=>decodeSemanticApplyRequest(value)).toThrow();});
 it('rejects preview on rejection and duplicate targets',()=>{const value=request();value.target_decisions[0].decision='reject';expect(()=>decodeSemanticApplyRequest(value)).toThrow();const duplicate=request();duplicate.target_decisions.push(duplicate.target_decisions[0]);expect(()=>decodeSemanticApplyRequest(duplicate)).toThrow();});
});


it('accepts a rejection without media and refuses explicit null preview for either decision',()=>{
 const reject={expected_revision:'1',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id(1),decision:'reject'}]};
 expect(decodeSemanticApplyRequest(reject)).toEqual(reject);
 for(const decision of ['reject','approve']) {
  expect(()=>decodeSemanticApplyRequest({...reject,target_decisions:[{target_id:id(1),decision,preview:null}]})).toThrow('SEMANTIC_APPLY_REQUEST_INVALID');
 }
});
