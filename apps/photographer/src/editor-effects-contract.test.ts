import {describe,it,expect} from 'vitest';
import {editorEffectsRequest,readEditorEffectsAccepted,readEditorEffectsStatus} from './editor-effects-contract';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const fixture=()=>({document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)}});
describe('Editor effects immutable admission',()=>{
 it('retains exact frozen identity and owns nested pins',()=>{const frozen=fixture(),command=editorEffectsRequest(frozen);frozen.source.source.sha256='d'.repeat(64);expect(command.source.source.sha256).toBe('a'.repeat(64));expect(command.frozen_document_id).toBe(id(1));expect(command.draft_revision).toBe(1);});
 it('rejects lossy revision conversion',()=>{expect(()=>editorEffectsRequest({...fixture(),draft_revision:'9007199254740992'})).toThrow();});
 it('never treats a collection Apply as a dedicated effects run',()=>{expect(()=>readEditorEffectsAccepted({run_id:id(6),output_collection_id:id(7),status:'queued'})).toThrow();expect(readEditorEffectsAccepted({run_id:id(6),output_collection_id:'00000000-0000-0000-0000-000000000000',status:'queued'})).toEqual({run_id:id(6),status:'queued'});});
 it('never identifies intermediate build or seal as final compose',()=>{
  const status={build_run_id:id(6),build_status:'completed',state:'complete',seal_run_id:id(7),final_run_id:id(8),review_required:false};
  expect(readEditorEffectsStatus(status,id(6)).final_run_id).toBe(id(8));
  for(const changed of [{final_run_id:id(6)},{final_run_id:id(7)},{seal_run_id:null},{final_run_id:null}])expect(()=>readEditorEffectsStatus({...status,...changed},id(6))).toThrow();
  expect(()=>readEditorEffectsStatus({...status,state:'sealed',final_run_id:null,seal_run_id:null},id(6))).toThrow();
 });
});

it('retains a cancelling build until ordinary cancellation and cleanup settle',()=>{
 const pending={build_run_id:id(6),build_status:'cancelling',state:'building',seal_run_id:null,final_run_id:null,review_required:false};
 expect(readEditorEffectsStatus(pending,id(6))).toEqual(pending);
 const settled={...pending,build_status:'cancelled',state:'failed'};
 expect(readEditorEffectsStatus(settled,id(6))).toEqual(settled);
 expect(()=>readEditorEffectsStatus({...pending,build_status:'invented'},id(6))).toThrow('EDITOR_EFFECT_STATUS_INVALID');
});

it('accepts intermediate completion with errors without presenting it as successful final compose',()=>{
 const failed={build_run_id:id(6),build_status:'completed_with_errors',state:'failed',seal_run_id:null,final_run_id:null,review_required:false};
 expect(readEditorEffectsStatus(failed,id(6))).toEqual(failed);
});

it('refuses coercible arrays for actual string status fields',()=>{
 const base={build_run_id:id(6),build_status:'running',state:'building',seal_run_id:null,final_run_id:null,review_required:false};
 for(const patch of [{state:['building']},{build_status:['running']}])expect(()=>readEditorEffectsStatus({...base,...patch},id(6))).toThrow('EDITOR_EFFECT_STATUS_INVALID');
});
