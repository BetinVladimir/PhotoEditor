import type {ArtifactRef} from './catalog-types';
export type InterchangeFormat='photoeditor_bundle'|'xmp'|'captureone_sidecar';
export type InterchangeCreateRequest={project_id:string;direction:'import';format:InterchangeFormat;bundle:ArtifactRef}|{project_id:string;direction:'export';format:InterchangeFormat;snapshot_id:string};
export type InterchangeCreateTicket={run_id:string;workflow_id:string;state:'queued'};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const invalid=():never=>{throw new Error('INTERCHANGE_CREATE_INVALID');};
/** Closed tagged union matching native CreateRequest; null/foreign fields fail. */
export function freezeInterchangeCreate(value:unknown,key:string):InterchangeCreateRequest{
 if(typeof key!=='string'||!/^[\x21-\x7e]{1,128}$/.test(key)||!value||typeof value!=='object'||Array.isArray(value))return invalid();
 const v=value as Record<string,unknown>;
 if(!uuid(v.project_id)||!['photoeditor_bundle','xmp','captureone_sidecar'].includes(v.format as string))return invalid();
 const format=v.format as InterchangeFormat;
 if(v.direction==='import'){
  if(!fields(v,['project_id','direction','format','bundle'])||!fields(v.bundle,['artifact_id','sha256'])||!uuid(v.bundle.artifact_id)||typeof v.bundle.sha256!=='string'||!/^[0-9a-f]{64}$/.test(v.bundle.sha256))return invalid();
  return {project_id:v.project_id,direction:'import',format,bundle:{artifact_id:v.bundle.artifact_id,sha256:v.bundle.sha256}};
 }
 if(v.direction==='export'){
  if(!fields(v,['project_id','direction','format','snapshot_id'])||!uuid(v.snapshot_id))return invalid();
  return {project_id:v.project_id,direction:'export',format,snapshot_id:v.snapshot_id};
 }
 return invalid();
}
export function readInterchangeCreateTicket(value:unknown):InterchangeCreateTicket{
 if(!fields(value,['run_id','workflow_id','state'])||!uuid(value.run_id)||!uuid(value.workflow_id)||value.state!=='queued')return invalid();
 return {run_id:value.run_id,workflow_id:value.workflow_id,state:'queued'};
}
