import type {RunOutput} from './catalog-types';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
export function readRunOutputs(raw:unknown):{items:RunOutput[]}{
 if(!raw||typeof raw!=='object'||Array.isArray(raw)||!Array.isArray((raw as {items?:unknown}).items))throw Error('RUN_OUTPUT_RESPONSE_INVALID');
 const seen=new Set<string>();const items=((raw as {items:unknown[]}).items).map(row=>{
  if(!row||typeof row!=='object'||Array.isArray(row))throw Error('RUN_OUTPUT_RESPONSE_INVALID');const v=row as RunOutput;
  if(!uuid(v.artifact_id)||seen.has(v.artifact_id)||typeof v.role!=='string'||!v.role||typeof v.sha256!=='string'||!/^[0-9a-f]{64}$/.test(v.sha256)||!Number.isSafeInteger(v.bytes)||v.bytes<1||typeof v.media_type!=='string'||!v.media_type||typeof v.expires_at!=='string'||!/^\d{4}-\d{2}-\d{2}T/.test(v.expires_at)||!Number.isFinite(Date.parse(v.expires_at))||typeof v.url!=='string'||v.url.length>16384)throw Error('RUN_OUTPUT_RESPONSE_INVALID');
  let url:URL;try{url=new URL(v.url)}catch{throw Error('RUN_OUTPUT_RESPONSE_INVALID')}
  const versions=url.searchParams.getAll('versionId');if(!['https:','http:'].includes(url.protocol)||url.username||url.password||url.hash||versions.length!==1||!versions[0]||versions[0]==='null'||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))throw Error('RUN_OUTPUT_RESPONSE_INVALID');
  seen.add(v.artifact_id);return structuredClone(v);
 });return {items};
}
