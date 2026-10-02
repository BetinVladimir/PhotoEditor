import {canonicalJSON} from '@photoeditor/api-client';
import {applyEditorCommand,currentEditorSnapshot,type EditorState} from './editor-store';
export type BatchSelection={versionId:string;sourceSha:string;draftRevision:number};
export type BatchParameter={path:string[];value:unknown};
export type BatchConflict={versionId:string;reason:'missing'|'source_changed'|'revision_changed'|'parameter_missing'};
/** Prepare per-source overrides; persistence must still use each source's server CAS. */
export function applyBatchCommand(selection:readonly BatchSelection[],states:ReadonlyMap<string,EditorState>,command:BatchParameter):{affectedCount:number;states:Map<string,EditorState>;conflicts:BatchConflict[]} {
 if(selection.length<1||selection.length>5000||!Array.isArray(command.path)||command.path.length<1||command.path.length>16||command.path.some(key=>typeof key!=='string'||!key||['__proto__','constructor','prototype'].includes(key)))throw new Error('BATCH_COMMAND_INVALID');
 const value=JSON.parse(canonicalJSON(command.value));
 const frozen=selection.map(item=>({...item})),seen=new Set<string>();
 for(const item of frozen){if(!item.versionId||!/^[0-9a-f]{64}$/.test(item.sourceSha)||!Number.isSafeInteger(item.draftRevision)||item.draftRevision<0||seen.has(item.versionId))throw new Error('BATCH_SELECTION_INVALID');seen.add(item.versionId);}
 const changed=new Map<string,EditorState>(),conflicts:BatchConflict[]=[];
 for(const item of frozen){
  const state=states.get(item.versionId);let reason:BatchConflict['reason']|undefined;
  if(!state)reason='missing';else if(state.versionId!==item.versionId||state.sourceSha!==item.sourceSha)reason='source_changed';else if(state.draftRevision!==item.draftRevision)reason='revision_changed';
  if(reason){conflicts.push({versionId:item.versionId,reason});continue;}
  const params=structuredClone(currentEditorSnapshot(state!).params);let target:unknown=params;
  for(const key of command.path.slice(0,-1)){if(!target||typeof target!=='object'||Array.isArray(target)||!Object.hasOwn(target,key)){target=undefined;break;}target=(target as Record<string,unknown>)[key];}
  const key=command.path[command.path.length-1];
  if(!target||typeof target!=='object'||Array.isArray(target)||!Object.hasOwn(target,key)){conflicts.push({versionId:item.versionId,reason:'parameter_missing'});continue;}
  (target as Record<string,unknown>)[key]=structuredClone(value);
  changed.set(item.versionId,applyEditorCommand(state!,{params}));
 }
 return {affectedCount:changed.size,states:changed,conflicts};
}
