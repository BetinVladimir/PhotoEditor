import {canonicalJSON} from '@photoeditor/api-client';
import {readExportBatchTicket,type ExportBatchTicket} from './export-batch-attempt-store';

type Group=ExportBatchTicket['groups'][number]&{run_status:string};
export type ExportBatchStatus={batch_id:string;project_id:string;state:'running'|'partial'|'failed'|'completed';groups:Group[]};
export function readExportBatchStatus(raw:unknown,expected:ExportBatchTicket):ExportBatchStatus{
 const fail=():never=>{throw Error('EXPORT_BATCH_STATUS_INVALID')};
 if(!raw||typeof raw!=='object'||Array.isArray(raw))return fail();
 const value=raw as ExportBatchStatus;
 if(Object.keys(value).sort().join(',')!=='batch_id,groups,project_id,state'||value.batch_id!==expected.batch_id||value.project_id!==expected.project_id||!Array.isArray(value.groups)||value.groups.length!==expected.groups.length)return fail();
 let pending=0,failed=0,complete=0;
 const groups=value.groups.map((group,index)=>{
  if(!group||Object.keys(group).sort().join(',')!=='failure_code,key,metadata_policy,package_id,presets,run_status,ticket')return fail();
  const {run_status,...ticket}=group,prior=expected.groups[index];
  if(group.key!==prior.key||group.package_id!==prior.package_id||group.metadata_policy!==prior.metadata_policy||canonicalJSON(group.presets)!==canonicalJSON(prior.presets)||prior.ticket!==null&&canonicalJSON(group.ticket)!==canonicalJSON(prior.ticket)||prior.failure_code!==null&&group.failure_code!==prior.failure_code)return fail();
  if(!['pending','unavailable','queued','running','blocked','completed','completed_with_errors','failed','cancelled','paused','cancelling','waiting_review'].includes(run_status))return fail();
  if(group.failure_code!==null){if(run_status!=='failed')return fail();failed++;}
  else if(group.ticket===null){if(run_status!=='pending')return fail();pending++;}
  else if(run_status==='pending')return fail();
  else if(run_status==='completed')complete++;
  else if(['failed','cancelled','completed_with_errors','unavailable'].includes(run_status))failed++;
  else pending++;
  return ticket;
 });
 // Reuse the full admission/group decoder before exposing any status fields.
 const admission=groups.some(g=>g.ticket===null&&g.failure_code===null)?'partial':groups.some(g=>g.failure_code!==null)?'failed':'queued';
 readExportBatchTicket({batch_id:value.batch_id,project_id:value.project_id,state:admission,groups},{snapshot_id:expected.batch_id,presets:expected.groups.flatMap(g=>g.presets)});
 const state=complete===groups.length?'completed':failed>0&&pending===0?'failed':failed>0?'partial':'running';
 if(value.state!==state)return fail();return structuredClone(value);
}
