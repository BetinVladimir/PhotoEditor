import {expect,it} from 'vitest';
import {freezeRelightPreparation,readPreparedRelight,type RelightPreparationRequest} from './relight-preparation';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
function request():RelightPreparationRequest{const ref={artifact_id:id,sha256:'a'.repeat(64)};return {schema_version:1,snapshot_id:id,ordinal:0,subject_kind:'manual',base:{asset_version_id:id,source:ref,canvas_hash:'b'.repeat(64)},depth:ref,subject:ref,protected:ref};}
it('owns selected references and accepts server-selected calibration only within source scope',()=>{
 const q=request(),owned=freezeRelightPreparation(q);expect(owned.depth).not.toBe(q.depth);
 const result={schema_version:1 as const,snapshot_id:q.snapshot_id,ordinal:0,subject_kind:q.subject_kind,params:{schema_version:1 as const,base:q.base,depth:q.depth,subject:q.subject,protected:q.protected,calibration:{artifact_id:other,sha256:'c'.repeat(64)},strength:.5,lights:[{x:.5,y:.5,radius:.3,ev:0}]}};
 expect(readPreparedRelight(result,q).params.calibration.artifact_id).toBe(other);
 expect(()=>readPreparedRelight({...result,snapshot_id:other},q)).toThrow('SCOPE_CHANGED');
 expect(()=>readPreparedRelight({...result,params:{...result.params,depth:{artifact_id:other,sha256:'a'.repeat(64)}}},q)).toThrow('SCOPE_CHANGED');
 expect(()=>freezeRelightPreparation({...q,calibration:q.depth} as RelightPreparationRequest)).toThrow();
});
