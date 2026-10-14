import {expect,it} from 'vitest';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {readSceneBoardDocument} from './scene-board';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,hash='a'.repeat(64);
const plan={schema_version:1,snapshot_id:id(1),snapshot_digest:hash,policy_sha:hash,groups:[{id:id(2),ordinals:[0],reference_ordinal:0,confidence:.8,corrections:{artifact_id:id(3),sha256:hash}}]};
const document={session_id:id(4),analysis_id:id(5),snapshot_id:id(1),snapshot_digest:hash,policy_sha:hash,plan_sha:bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(plan)))),plan,frames:[{ordinal:0,asset_id:id(8),version_id:id(6),source_sha:hash}]};
it('checks the exact plan digest and route session/analysis before creating a board',()=>{const board=readSceneBoardDocument(document,id(5),id(4));expect(board.groups[0].reference.versionId).toBe(id(6));expect(board.planSha).toBe(document.plan_sha);expect(board.groups[0].frames[0].assetId).toBe(id(8));});
it('rejects a rebound route and modified plan before display',()=>{for(const value of [{...document,session_id:id(7)},{...document,analysis_id:id(7)},{...document,plan_sha:hash},{...document,plan:{...plan,groups:[]}}])expect(()=>readSceneBoardDocument(value,id(5),id(4))).toThrow();});

it('rejects invalid asset identity in the media projection',()=>{expect(()=>readSceneBoardDocument({...document,frames:[{...document.frames[0],asset_id:"invalid"}]},id(5),id(4))).toThrow();});
