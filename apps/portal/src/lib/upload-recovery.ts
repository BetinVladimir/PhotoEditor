import * as api from './api';
import {readUpload,readUploadAccepted} from './response';
import type {Upload} from './types';
type Part={part_no:number;etag:string};
type Attempt={schema:1;session:string;sha:string;name:string;size:number;beginKey:string;completeKey:string;upload:Upload|null;parts:Part[];accepted:boolean;receipt:{job_id:string;status:string}|null};
const confirmed=new Map<string,Attempt>();
const key=(share:string)=>`photo:portal:${share}:upload-attempt`;
function decode(raw:string):Attempt{
 if(raw.length>1048576)throw Error('INVALID_UPLOAD_JOURNAL');
 const v=JSON.parse(raw) as Attempt;
 const fields=['schema','session','sha','name','size','beginKey','completeKey','upload','parts','accepted','receipt'];
 const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/i;
 if(!v||typeof v!=='object'||Object.keys(v).length!==fields.length||Object.keys(v).some(k=>!fields.includes(k))||v.schema!==1||typeof v.session!=='string'||!v.session||v.session.length>4096||!/^[a-f0-9]{64}$/.test(v.sha)||typeof v.name!=='string'||!v.name||v.name.length>1024||!Number.isSafeInteger(v.size)||v.size<=0||v.size>536870912||!Array.isArray(v.parts)||v.parts.length>10000||typeof v.accepted!=='boolean'||![v.beginKey,v.completeKey].every(k=>typeof k==='string'&&uuid.test(k)))throw Error('INVALID_UPLOAD_JOURNAL');
 if(v.upload!==null)v.upload=readUpload(v.upload,undefined,{filename:v.name,bytes:v.size});
 const seen=new Set<number>();for(const p of v.parts){if(!p||Object.keys(p).length!==2||!Object.prototype.hasOwnProperty.call(p,'part_no')||!Object.prototype.hasOwnProperty.call(p,'etag')||!Number.isInteger(p.part_no)||p.part_no<1||!v.upload||p.part_no>v.upload.parts_total||seen.has(p.part_no)||typeof p.etag!=='string'||!p.etag||p.etag.length>1024)throw Error('INVALID_UPLOAD_JOURNAL');seen.add(p.part_no);}
 if(v.receipt!==null)v.receipt=readUploadAccepted(v.receipt);
 if(v.accepted!==(v.receipt!==null)||!v.upload&&(v.parts.length>0||v.accepted)||v.accepted&&seen.size!==v.upload!.parts_total)throw Error('INVALID_UPLOAD_JOURNAL');
 return v;
}

/** Same file bytes and immutable begin/complete keys survive ambiguous responses.
 * Stopping observation never claims abort/rollback or removes an unknown intent.
 */
export async function uploadRecoverably(storage:Storage,share:string,csrf:string,file:File,signal:AbortSignal,current:()=>boolean){
 const check=()=>{signal.throwIfAborted();if(!current())throw Error('UPLOAD_SCOPE_WITHDRAWN');};
 check();if(!Number.isSafeInteger(file.size)||file.size<=0||file.size>536870912||!file.name||file.name.length>1024)throw Error('UPLOAD_FILE_SIZE');
 const bytes=await file.arrayBuffer();check();
 const hash=await crypto.subtle.digest('SHA-256',bytes);check();
 const sha=Array.from(new Uint8Array(hash),b=>b.toString(16).padStart(2,'0')).join('');
 const sessionDigest=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(csrf));check();
 const scope=key(share)+':'+Array.from(new Uint8Array(sessionDigest),b=>b.toString(16).padStart(2,'0')).join('');
 const raw=storage.getItem(scope);let attempt=raw?decode(raw):null;
 const known=confirmed.get(scope);
 if(known){if(attempt&&(attempt.beginKey!==known.beginKey||attempt.completeKey!==known.completeKey||attempt.sha!==known.sha))throw Error('UPLOAD_JOURNAL_DIVERGED');attempt=known;}
 if(attempt&&attempt.session!==csrf)throw Error('UPLOAD_SESSION_CHANGED');
 if(attempt?.accepted){if(attempt.sha===sha&&attempt.name===file.name&&attempt.size===file.size)return;storage.removeItem(scope);confirmed.delete(scope);attempt=null;}
 if(attempt&&(attempt.sha!==sha||attempt.name!==file.name||attempt.size!==file.size))throw Error('UPLOAD_RESELECT_ORIGINAL_FILE');
 attempt??={schema:1,session:csrf,sha,name:file.name,size:file.size,beginKey:api.idempotencyKey(),completeKey:api.idempotencyKey(),upload:null,parts:[],accepted:false,receipt:null};
 const save=()=>{check();storage.setItem(scope,JSON.stringify(attempt));};save();
 if(!attempt.upload){attempt.upload=await api.beginUpload(csrf,file,signal,attempt.beginKey);save();}
 const upload=attempt.upload;
 for(let offset=0;offset<upload.parts_total;offset+=100){
  check();const numbers=Array.from({length:Math.min(100,upload.parts_total-offset)},(_,i)=>offset+i+1).filter(n=>!attempt!.parts.some(p=>p.part_no===n));
  if(!numbers.length)continue;
  const signed=await api.signParts(csrf,upload.id,numbers,signal);check();
  for(const part of signed.items){
   check();const start=(part.part_no-1)*upload.part_bytes;
   const response=await fetch(part.url,{method:'PUT',body:file.slice(start,Math.min(start+upload.part_bytes,file.size)),signal});check();
   const etag=response.headers.get('etag');if(!response.ok||!etag)throw Error('UPLOAD_PART_FAILED');
   attempt.parts.push({part_no:part.part_no,etag});attempt.parts.sort((a,b)=>a.part_no-b.part_no);save();
  }
 }
 check();const receipt=await api.completeUpload(csrf,upload,attempt.parts,signal,attempt.completeKey);check();attempt.receipt=receipt;attempt.accepted=true;confirmed.set(scope,attempt);save();
}
