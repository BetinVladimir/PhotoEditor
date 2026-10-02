import {expect,it} from 'vitest';
import {sceneReferenceRequest} from './scene-analysis-request';
import type {SceneBoard} from './scene-board';
import type {ShootSession} from './capture-types';
const frame=(ordinal:number)=>({ordinal,versionId:'version'+ordinal,sourceSha:'a'.repeat(64)});
const board:SceneBoard={snapshotId:'snapshot',snapshotDigest:'a'.repeat(64),policySha:'b'.repeat(64),groups:[{id:'group0',ordinals:[0,1],frames:[frame(0),frame(1)],reference:frame(0),confidence:1,corrections:{artifact_id:'correction',sha256:'a'.repeat(64)},outliers:null},{id:'group1',ordinals:[2],frames:[frame(2)],reference:frame(2),confidence:1,corrections:{artifact_id:'correction2',sha256:'a'.repeat(64)},outliers:null}]};
it('preserves other references and freezes the session policy',()=>{
 const session={state:'open',revision:'4',policy:{schema_version:1,camera_offsets:[]}} as unknown as ShootSession;
 const request=sceneReferenceRequest(board,'analysis',session,'selection','group0','version1');expect(request.manual_references.map(ref=>ref.asset_version_id)).toEqual(['version1','version2']);expect(board.groups[0].reference.versionId).toBe('version0');session.policy.camera_offsets.push({camera_hash:'a'.repeat(64),seconds:1});expect(request.policy.camera_offsets).toEqual([]);
});
it('rejects references outside a group and closed sessions',()=>{
 const session={state:'open',revision:'4',policy:{}} as ShootSession;expect(()=>sceneReferenceRequest(board,'analysis',session,'selection','group0','version2')).toThrow();expect(()=>sceneReferenceRequest(board,'analysis',{...session,state:'closed'},'selection','group0','version1')).toThrow();
});
