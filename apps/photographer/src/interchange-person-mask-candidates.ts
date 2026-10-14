import {freezeImportPersonMaskDecision,type ImportPersonMaskDecision} from './interchange-person-mask-decision';

const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const sha=(value:unknown)=>typeof value==='string'&&/^[0-9a-f]{64}$/.test(value);
const record=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
const keys=(value:Record<string,unknown>,expected:readonly string[])=>Object.keys(value).length===expected.length&&expected.every(key=>Object.prototype.hasOwnProperty.call(value,key));

export type ImportPersonMaskCandidate={external_id:string;decision:ImportPersonMaskDecision};
export type ImportPersonMaskCandidatePage={workflow_id:string;entry_id:string;candidates:ImportPersonMaskCandidate[]};
export type ImportPersonMaskEntry={entry_id:string;target_version_id:string;asset_id:string;external_id:string};

export function readImportPersonMaskEntries(value:unknown,workflow:string):{workflow_id:string;entries:ImportPersonMaskEntry[]}{
  if(!uuid(workflow)||!record(value)||!keys(value,['workflow_id','entries'])||value.workflow_id!==workflow||!Array.isArray(value.entries)||value.entries.length>5000)throw new Error('INTERCHANGE_MASK_ENTRIES_INVALID');
  const entries:ImportPersonMaskEntry[]=[],seen=new Set<string>();
  for(const raw of value.entries){
    if(!record(raw)||!keys(raw,['entry_id','target_version_id','asset_id','external_id'])||!uuid(raw.entry_id)||!uuid(raw.target_version_id)||!uuid(raw.asset_id)||typeof raw.external_id!=='string'||raw.external_id.length<1||raw.external_id.length>240||seen.has(raw.entry_id))throw new Error('INTERCHANGE_MASK_ENTRIES_INVALID');
    seen.add(raw.entry_id);entries.push({entry_id:raw.entry_id,target_version_id:raw.target_version_id,asset_id:raw.asset_id,external_id:raw.external_id});
  }
  return {workflow_id:workflow,entries};
}

export function readImportPersonMaskCandidates(value:unknown,workflow:string,entry:string):ImportPersonMaskCandidatePage{
  if(!uuid(workflow)||!uuid(entry)||!record(value)||!keys(value,['workflow_id','entry_id','candidates'])||value.workflow_id!==workflow||value.entry_id!==entry||!Array.isArray(value.candidates)||value.candidates.length>5000)throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');
  const candidates:ImportPersonMaskCandidate[]=[],seen=new Set<string>();
  for(const raw of value.candidates){
    if(!record(raw)||!keys(raw,['external_id','decision'])||typeof raw.external_id!=='string'||raw.external_id.length<1||raw.external_id.length>240||!record(raw.decision)||!keys(raw.decision,['action','entry_id','target_version_id','override_artifact_id','mask_artifact_id','parsed_jcs','recipe_artifact_id','recipe_jcs','override_jcs','mask_jcs']))throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');
    const decision=freezeImportPersonMaskDecision(raw.decision as ImportPersonMaskDecision);
    if(decision.action!=='grant'||decision.entry_id!==entry)throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');
    let mask:unknown;
    try{mask=JSON.parse(decision.mask_jcs!);}catch{throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');}
    if(!record(mask)||mask.selector!=='person'||mask.object_id!==null||!sha(mask.model_lock_id)||!record(mask.artifact)||mask.artifact.artifact_id!==decision.mask_artifact_id||!sha(mask.artifact.sha256))throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');
    const identity=JSON.stringify([decision.override_artifact_id,decision.mask_artifact_id,decision.target_version_id]);
    if(seen.has(identity))throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');seen.add(identity);
    candidates.push({external_id:raw.external_id,decision});
  }
  return {workflow_id:workflow,entry_id:entry,candidates};
}
