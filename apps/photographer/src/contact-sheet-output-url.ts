import type {RunOutput} from './catalog-types';
/** Accept immutable signed PNG URLs issued by the run output catalog. */
export function contactSheetOutputURL(item:RunOutput,now=Date.now()):string{
 const invalid=():never=>{throw new Error('CONTACT_OUTPUT_INVALID');};
 if(item.role!=='contact_page'||item.media_type!=='image/png'||!item.artifact_id||!/^[a-f0-9]{64}$/.test(item.sha256)||!Number.isSafeInteger(item.bytes)||item.bytes<=0||!Number.isFinite(Date.parse(item.expires_at))||Date.parse(item.expires_at)<=now||typeof item.url!=='string'||item.url.length>16384)invalid();
 let url:URL;try{url=new URL(item.url);}catch{return invalid();}
 const versions=url.searchParams.getAll('versionId');
 if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||versions.length!==1||!versions[0]||versions[0]==='null'||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))invalid();
 return url.href;
}
