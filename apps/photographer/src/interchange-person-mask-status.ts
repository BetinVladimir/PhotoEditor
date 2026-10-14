const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const record=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
export type ImportPersonMaskStatus={entry_id:string;override_artifact_id:string;mask_artifact_id:string;target_version_id:string;status:'granted'|'revoked';epoch:number};
export function readImportPersonMaskStatuses(value:unknown,workflow:string):ImportPersonMaskStatus[]{
  if(!record(value)||value.workflow_id!==workflow||!uuid(workflow)||!Array.isArray(value.items)||value.items.length>5000||Object.keys(value).length!==2)throw new Error('INTERCHANGE_MASK_STATUS_INVALID');
  const seen=new Set<string>();
  return value.items.map((raw:unknown)=>{
    if(!record(raw)||Object.keys(raw).length!==6||!uuid(raw.entry_id)||!uuid(raw.override_artifact_id)||!uuid(raw.mask_artifact_id)||!uuid(raw.target_version_id)||!['granted','revoked'].includes(String(raw.status))||!Number.isSafeInteger(raw.epoch)||Number(raw.epoch)<1)throw new Error('INTERCHANGE_MASK_STATUS_INVALID');
    const key=JSON.stringify([raw.entry_id,raw.override_artifact_id,raw.mask_artifact_id,raw.target_version_id]);
    if(seen.has(key))throw new Error('INTERCHANGE_MASK_STATUS_INVALID');seen.add(key);
    return {entry_id:raw.entry_id,override_artifact_id:raw.override_artifact_id,mask_artifact_id:raw.mask_artifact_id,target_version_id:raw.target_version_id,status:raw.status,epoch:raw.epoch} as ImportPersonMaskStatus;
  });
}
