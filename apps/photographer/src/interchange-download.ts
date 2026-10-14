import type {RunOutput} from './catalog-types';
import {assertInterchangeSnapshot,type InterchangeJob,type InterchangeInspection} from './interchange-job';

/** Validate a fresh server-signed archive link without buffering archive bytes. */
export function readInterchangeDownload(value:unknown,job:InterchangeJob,inspection:InterchangeInspection,now=Date.now()):RunOutput {
 assertInterchangeSnapshot(job,inspection);
 if(job.direction!=='export'||!job.bundle_artifact_id||inspection.status!=='completed'||inspection.stage!=='completed')throw new Error('INTERCHANGE_DOWNLOAD_UNAVAILABLE');
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('INTERCHANGE_DOWNLOAD_INVALID');
 const items=(value as {items?:unknown}).items;
 if(!Array.isArray(items)||items.length>1000)throw new Error('INTERCHANGE_DOWNLOAD_INVALID');
 const matches=items.filter(item=>item&&typeof item==='object'&&item.artifact_id===job.bundle_artifact_id);
 if(matches.length!==1)throw new Error('INTERCHANGE_DOWNLOAD_UNAVAILABLE');
 const ref=matches[0];
 if(ref.role!=='bundle'||ref.media_type!=='application/zip'||!Number.isSafeInteger(ref.bytes)||ref.bytes<22||ref.bytes>68719476736||typeof ref.sha256!=='string'||!/^[0-9a-f]{64}$/.test(ref.sha256)||typeof ref.expires_at!=='string'||!Number.isFinite(now)||!Number.isFinite(Date.parse(ref.expires_at))||Date.parse(ref.expires_at)<=now+5000||typeof ref.url!=='string'||ref.url.length>16384)throw new Error('INTERCHANGE_DOWNLOAD_INVALID');
 let url:URL;try{url=new URL(ref.url);}catch{throw new Error('INTERCHANGE_DOWNLOAD_INVALID');}
 if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||
    url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))throw new Error('INTERCHANGE_DOWNLOAD_INVALID');
 return {artifact_id:ref.artifact_id,role:ref.role,sha256:ref.sha256,bytes:ref.bytes,media_type:ref.media_type,url:url.href,expires_at:ref.expires_at};
}
