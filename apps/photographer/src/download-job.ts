export type DownloadJob={error?:unknown;expires_at?:string;id:string;status:string;files_done:number;files_total:number;bytes_written:number;volumes:{number:number;status:string;bytes:number;delivery_kind:'single'|'zip'}[]};
export const downloadJobTerminal=(state:string)=>['ready','failed','cancelled','expired'].includes(state);
export function readDownloadJob(value:unknown,id:string):DownloadJob{
 const v=value as DownloadJob|null;
 if(!v||typeof v!=='object'||Array.isArray(v)||Object.keys(v).some(key=>!['id','status','files_done','files_total','bytes_written','volumes','expires_at','error'].includes(key))||v.expires_at!==undefined&&(typeof v.expires_at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(v.expires_at)||!Number.isFinite(Date.parse(v.expires_at)))||v.id!==id||!['queued','preparing','building','uploading','cancelling','ready','failed','cancelled','expired'].includes(v.status)||!Number.isSafeInteger(v.files_done)||!Number.isSafeInteger(v.files_total)||v.files_done<0||v.files_total<v.files_done||!Number.isSafeInteger(v.bytes_written)||v.bytes_written<0||!Array.isArray(v.volumes)||v.volumes.length>5000)throw new Error('DOWNLOAD_JOB_INVALID');
 const numbers=new Set<number>();
 for(const volume of v.volumes){
  if(!volume||typeof volume!=='object'||Array.isArray(volume)||Object.keys(volume).sort().join(',')!=='bytes,delivery_kind,number,status'||!Number.isInteger(volume.number)||volume.number<1||volume.number>5000||numbers.has(volume.number)||typeof volume.status!=='string'||!Number.isSafeInteger(volume.bytes)||volume.bytes<0||!['single','zip'].includes(volume.delivery_kind))throw new Error('DOWNLOAD_JOB_INVALID');
  numbers.add(volume.number);
 }
 return structuredClone(v);
}

export function readDownloadAccepted(value:unknown):{download_job_id:string;status:'queued'} {
 const row=value as Record<string,unknown>|null;
 if(!row||typeof row!=='object'||Array.isArray(row)||Object.keys(row).sort().join(',')!=='download_job_id,status'||typeof row.download_job_id!=='string'||! /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(row.download_job_id)||row.download_job_id==='00000000-0000-0000-0000-000000000000'||row.status!=='queued')throw Error('DOWNLOAD_JOB_INVALID');
 return {download_job_id:row.download_job_id,status:'queued'};
}
