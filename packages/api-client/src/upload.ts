import {rawUploadPolicy,type RAWDevelopment} from './raw-development';
export type UploadState="queued"|"uploading"|"paused"|"completing"|"complete"|"error"|"aborted";
export type UploadRecord={rawDevelopment?:RAWDevelopment;localId:string;tenant:string;projectId:string;collectionId?:string;uploadId?:string;name:string;size:number;lastModified:number;sha256:string;partBytes?:number;partsTotal?:number;parts:Array<{part_no:number;etag:string}>;state:UploadState;error?:string};
export interface UploadSource{name:string;size:number;lastModified:number;sha256:string;verify?(signal:AbortSignal):Promise<string>;read(start:number,end:number,signal:AbortSignal):Promise<Uint8Array>}
export interface UploadPersistence{load():Promise<UploadRecord[]>;save(records:UploadRecord[]):Promise<void>}
export type RemoteUploadState="initializing"|"uploading"|"completing"|"verifying"|"verified"|"rejected"|"aborted"|"expired";
export interface MultipartAPI{status?(record:UploadRecord,signal:AbortSignal):Promise<{id:string;part_bytes:number;parts_total:number;state:RemoteUploadState}>;begin(record:UploadRecord,signal:AbortSignal):Promise<{id:string;part_bytes:number;parts_total:number}>;sign(uploadId:string,parts:number[],signal:AbortSignal):Promise<Array<{part_no:number;url:string}>>;put(url:string,bytes:Uint8Array,signal:AbortSignal):Promise<string>;complete(uploadId:string,parts:Array<{part_no:number;etag:string}>,expectedSize:number,signal:AbortSignal):Promise<void>;abort(uploadId:string,signal:AbortSignal):Promise<void>}
export const uploadRequiresRestart=(record:Pick<UploadRecord,"error">)=>record.error==="UPLOAD_EXPIRED"||record.error==="UPLOAD_REJECTED";
const sameFile=(r:UploadRecord,s:UploadSource)=>r.name===s.name&&r.size===s.size&&r.lastModified===s.lastModified&&r.sha256===s.sha256;
function copyUploadRecord(record:UploadRecord):UploadRecord {
 return {...record,parts:record.parts.map(part=>({...part})),...(record.rawDevelopment?{rawDevelopment:{...record.rawDevelopment}}:{})};
}

// Own the admitted methods and identity; caller mutation must never silently
// remove final verification or substitute a different source while an attempt waits.
function admittedSource(source:UploadSource):UploadSource {
 const {name,size,lastModified,sha256,read,verify}=source;
 if(typeof read!=="function"||(verify!==undefined&&typeof verify!=="function"))throw new Error("invalid upload source");
 const check=()=>{if(source.name!==name||source.size!==size||source.lastModified!==lastModified||source.sha256!==sha256||source.read!==read||source.verify!==verify)throw new Error("UPLOAD_FILE_MISMATCH")};
 return Object.freeze({name,size,lastModified,sha256,
  read:async(start:number,end:number,signal:AbortSignal)=>{check();const bytes=await read.call(source,start,end,signal);check();return bytes},
  ...(verify?{verify:async(signal:AbortSignal)=>{check();const digest=await verify.call(source,signal);check();return digest}}:{})});
}
// Persisted multipart checkpoints are untrusted input, including their layout.
function checkedParts(record:UploadRecord){
  if(!Array.isArray(record.parts))throw new Error("UPLOAD_CHECKPOINT_INVALID");
  const hasSession=record.uploadId!==undefined;
  if(hasSession){
    if(typeof record.uploadId!=="string"||!record.uploadId||record.uploadId.length>128||
       !Number.isSafeInteger(record.size)||record.size<1||record.size>536870912||
       !Number.isSafeInteger(record.partBytes)||!record.partBytes||record.partBytes<1||record.partBytes>16777216||
       !Number.isSafeInteger(record.partsTotal)||!record.partsTotal||record.partsTotal<1||record.partsTotal>10000||
       record.partsTotal!==Math.ceil(record.size/record.partBytes))throw new Error("UPLOAD_CHECKPOINT_INVALID");
  }else if(record.partBytes!==undefined||record.partsTotal!==undefined||record.parts.length)throw new Error("UPLOAD_CHECKPOINT_INVALID");
  const seen=new Set<number>();
  const parts=record.parts.map(part=>{
    if(!part||!Number.isSafeInteger(part.part_no)||part.part_no<1||part.part_no>record.partsTotal!||seen.has(part.part_no)||
       typeof part.etag!=="string"||!part.etag||part.etag.length>256||/[\r\n\x00]/.test(part.etag))throw new Error("UPLOAD_CHECKPOINT_INVALID");
    seen.add(part.part_no);return{part_no:part.part_no,etag:part.etag};
  });
  return parts.sort((a,b)=>a.part_no-b.part_no);
}
function checkedMetadata(record:UploadRecord){
  const text=(value:unknown)=>typeof value==="string"&&value.length>0&&value.length<=256&&!/[\r\n\x00]/.test(value);
  if(!record||!text(record.localId)||!text(record.tenant)||!text(record.projectId)||
     (record.collectionId!==undefined&&!text(record.collectionId))||
     typeof record.name!=="string"||!record.name.trim()||[...record.name].length>255||/[\x00/\\]/.test(record.name)||
     !Number.isSafeInteger(record.size)||record.size<1||record.size>536870912||
     !Number.isSafeInteger(record.lastModified)||record.lastModified<0||
     typeof record.sha256!=="string"||!/^[0-9a-f]{64}$/.test(record.sha256)||
     !["queued","uploading","paused","completing","complete","error","aborted"].includes(record.state)||
     (record.error!==undefined&&(typeof record.error!=="string"||record.error.length>2048)))throw new Error("UPLOAD_CHECKPOINT_INVALID");
  if((record.state==="complete"||record.state==="completing")&&!record.uploadId)throw new Error("UPLOAD_CHECKPOINT_INVALID");
}
export class UploadQueue{
  records:UploadRecord[]=[];private sources=new Map<string,UploadSource>();private running=new Map<string,AbortController>();
  private listeners=new Set<()=>void>();private closed=false;private restoring=false;private saves:Promise<void>=Promise.resolve();private refreshing=new Map<string,AbortController>();private aborting=new Map<string,AbortController>();
  private active(signal?:AbortSignal){if(this.closed)throw new Error("UPLOAD_QUEUE_CLOSED");signal?.throwIfAborted()}
  private writable(){this.active();if(this.restoring)throw new Error("UPLOAD_RESTORE_IN_PROGRESS")}
  /** Local shutdown preserves server uploads and durable idempotency keys. */
  close(){if(this.closed)return;this.closed=true;for(const controller of this.running.values())controller.abort();for(const controller of this.refreshing.values())controller.abort();for(const controller of this.aborting.values())controller.abort();this.sources.clear();this.listeners.clear();for(const r of this.records)if(r.state!=="complete"&&r.state!=="aborted")r.state="paused"}
  private schedule(){if(!this.closed)void this.pump().catch(()=>{/* pump records persistence errors before returning */})}
  constructor(private api:MultipartAPI,private persistence:UploadPersistence,private maxFiles=2,private maxParts=2){if(!Number.isInteger(maxFiles)||!Number.isInteger(maxParts)||maxFiles<1||maxFiles>2||maxParts<1||maxParts>2)throw new Error("unsafe upload concurrency")}
  subscribe(listener:()=>void){this.listeners.add(listener);return()=>this.listeners.delete(listener)}
  async restore(){
    this.writable();if(this.records.length||this.running.size||this.refreshing.size||this.aborting.size)throw new Error("UPLOAD_BUSY");
    this.restoring=true;
    try{const loaded=await this.persistence.load();this.active();if(!Array.isArray(loaded)||new Set(loaded.map(x=>x?.localId)).size!==loaded.length||loaded.some(x=>!x||typeof x.localId!=="string"||!x.localId))throw new Error("UPLOAD_CHECKPOINT_INVALID");for(const record of loaded)checkedMetadata(record);const restored:UploadRecord[]=loaded.map(x=>({...x,state:x.state==="complete"||x.state==="aborted"||x.state==="completing"?x.state:"paused",parts:checkedParts(x),...(x.rawDevelopment!==undefined?{rawDevelopment:rawUploadPolicy(x.name,x.rawDevelopment)}:{})}));this.records=restored;await this.persist();return this.records}finally{this.restoring=false}
  }
  async enqueue(source:UploadSource,scope:{tenant:string;projectId:string;collectionId?:string;rawDevelopment?:RAWDevelopment}){this.writable();source=admittedSource(source);if(!source.name||!Number.isSafeInteger(source.size)||source.size<1||source.size>536870912||!/^[0-9a-f]{64}$/.test(source.sha256))throw new Error("invalid upload source");const record:UploadRecord={localId:crypto.randomUUID(),...scope,...(scope.rawDevelopment!==undefined?{rawDevelopment:rawUploadPolicy(source.name,scope.rawDevelopment)}:{}),name:source.name,size:source.size,lastModified:source.lastModified,sha256:source.sha256,parts:[],state:"queued"};checkedMetadata(record);this.records.push(record);this.sources.set(record.localId,source);await this.persist();this.schedule();return record}
  async resume(localId:string,source:UploadSource){this.writable();source=admittedSource(source);const r=this.require(localId);if(!sameFile(r,source))throw new Error("UPLOAD_FILE_MISMATCH");if(r.state==="complete"||r.state==="aborted")return;if(uploadRequiresRestart(r))throw new Error("UPLOAD_RESTART_REQUIRED");const running=this.running.get(localId);if((running&&!running.signal.aborted)||this.refreshing.has(localId)||this.aborting.has(localId))throw new Error("UPLOAD_BUSY");r.state="queued";r.error=undefined;this.sources.set(localId,source);await this.persist();this.schedule()}
  async pause(localId:string){this.writable();this.running.get(localId)?.abort();this.refreshing.get(localId)?.abort();const r=this.require(localId);if(r.state!=="complete"&&r.state!=="aborted")r.state="paused";await this.persist()}
  async abort(localId:string){
    this.writable();const r=this.require(localId);
    if(r.state==="complete"||r.state==="aborted")return;
    if(this.aborting.has(localId))throw new Error("UPLOAD_BUSY");
    const attempt=this.running.get(localId);
    attempt?.abort();this.refreshing.get(localId)?.abort();
    const c=new AbortController();this.aborting.set(localId,c);r.state="paused";
    try{
      await this.persist();this.active(c.signal);
      // Durable records without a session ID may represent a lost Begin reply.
      // Recover using the original key before asserting a remote cancellation.
      if(!r.uploadId){
        const recovered=await this.api.begin(copyUploadRecord(r),c.signal);
        this.active(c.signal);
        const candidate={...r,uploadId:recovered.id,partBytes:recovered.part_bytes,partsTotal:recovered.parts_total};
        checkedParts(candidate);
        r.uploadId=candidate.uploadId;r.partBytes=candidate.partBytes;r.partsTotal=candidate.partsTotal;
        await this.persist();this.active(c.signal);
      }
      if(this.api.status){const state=await this.current(r,c);if(state!=="uploading"&&state!=="initializing"){this.remote(r,state);await this.persist();return}}
      if(r.uploadId)await this.api.abort(r.uploadId,c.signal);
      this.active(c.signal);r.state="aborted";r.error=undefined;this.sources.delete(localId);await this.persist();
    }catch(e){
      if(!this.closed&&!c.signal.aborted){r.state="error";r.error=e instanceof Error?e.message:"UPLOAD_ABORT_FAILED";await this.persist()}
      throw e;
    }finally{this.aborting.delete(localId)}
  }
  private remote(r:UploadRecord,state:RemoteUploadState){
    if(state==="uploading"){r.error=undefined;return false;}
    if(state==="verified"){r.state="complete";r.error=undefined;}
    else if(state==="completing"||state==="verifying"){r.state="completing";r.error=undefined;}
    else if(state==="aborted"){r.state="aborted";r.error=undefined;}
    else if(state==="initializing"){r.state="paused";r.error=undefined;}
    else if(state==="rejected"||state==="expired"){r.state="error";r.error=state==="rejected"?"UPLOAD_REJECTED":"UPLOAD_EXPIRED"}
    else throw new Error("invalid upload state");
    this.sources.delete(r.localId);return true;
  }
  private async current(r:UploadRecord,c:AbortController){
    this.active(c.signal);if(!this.api.status)return "uploading" as const;
    const current=await this.api.status(copyUploadRecord(r),c.signal);this.active(c.signal);
    if(current.id!==r.uploadId||current.part_bytes!==r.partBytes||current.parts_total!==r.partsTotal)throw new Error("upload session changed");
    return current.state;
  }
  async refresh(localId:string){
    this.writable();const r=this.require(localId);if(!r.uploadId||!this.api.status||this.running.has(localId)||this.refreshing.has(localId)||this.aborting.has(localId)||r.state==="complete"||r.state==="aborted")return;
    const c=new AbortController();this.refreshing.set(localId,c);
    try{const state=await this.current(r,c);if(!this.remote(r,state))r.state="paused";await this.persist()}finally{this.refreshing.delete(localId)}
  }
  private async pump(){while(!this.closed&&this.running.size<this.maxFiles){
    const r=this.records.find(x=>x.state==="queued"&&this.sources.has(x.localId)&&!this.running.has(x.localId)&&!this.aborting.has(x.localId));if(!r)return;
    const source=this.sources.get(r.localId)!;const c=new AbortController();this.running.set(r.localId,c);r.state="uploading";
    try{await this.persist();this.active(c.signal)}catch(e){this.running.delete(r.localId);if(!this.closed){if(c.signal.aborted){if(r.state!=="queued"&&r.state!=="aborted")r.state="paused";this.schedule()}else{r.state="error";r.error=e instanceof Error?e.message:"UPLOAD_FAILED";for(const listener of this.listeners)listener()}}return}
    void this.process(r,source,c).finally(()=>{this.running.delete(r.localId);this.schedule()}).catch(()=>{/* persistence error is retained on the record */});
  }}
  private async process(r:UploadRecord,s:UploadSource,c:AbortController){try{this.active(c.signal);if(!r.uploadId){const begun=await this.api.begin(copyUploadRecord(r),c.signal);this.active(c.signal);if(!begun||typeof begun.id!=="string"||!begun.id||begun.id.length>128||!Number.isSafeInteger(begun.part_bytes)||begun.part_bytes<1||begun.part_bytes>16777216||!Number.isSafeInteger(begun.parts_total)||begun.parts_total<1||begun.parts_total>10000||begun.parts_total!==Math.ceil(r.size/begun.part_bytes))throw new Error("invalid upload session");r.uploadId=begun.id;r.partBytes=begun.part_bytes;r.partsTotal=begun.parts_total;await this.persist()}this.active(c.signal);let remote=await this.current(r,c);if(remote==="initializing"){const replay=await this.api.begin(copyUploadRecord(r),c.signal);this.active(c.signal);if(replay.id!==r.uploadId||replay.part_bytes!==r.partBytes||replay.parts_total!==r.partsTotal)throw new Error("upload session changed");remote=await this.current(r,c)}if(this.remote(r,remote)){await this.persist();return}this.active(c.signal);if(!Number.isSafeInteger(r.partBytes)||!r.partBytes||r.partBytes<1||r.partBytes>16777216||!Number.isSafeInteger(r.partsTotal)||!r.partsTotal||r.partsTotal<1||r.partsTotal>10000||r.partsTotal!==Math.ceil(r.size/r.partBytes)||!r.uploadId)throw new Error("invalid upload session");r.parts=checkedParts(r);const done=new Set(r.parts.map(x=>x.part_no));const pending=Array.from({length:r.partsTotal},(_,i)=>i+1).filter(x=>!done.has(x));for(let offset=0;offset<pending.length;offset+=this.maxParts){this.active(c.signal);const batch=pending.slice(offset,offset+this.maxParts);const links=await this.api.sign(r.uploadId,[...batch],c.signal);this.active(c.signal);if(!Array.isArray(links)||links.length!==batch.length||new Set(links.map(x=>x?.part_no)).size!==batch.length||links.some(link=>!link||!Number.isSafeInteger(link.part_no)||!batch.includes(link.part_no)||typeof link.url!=="string"||!link.url||link.url.length>32768))throw new Error("invalid signed part batch");const ownedLinks=links.map(link=>({part_no:link.part_no,url:link.url}));const settled=await Promise.allSettled(ownedLinks.map(async link=>{const start=(link.part_no-1)*r.partBytes!;this.active(c.signal);const bytes=await s.read(start,Math.min(r.size,start+r.partBytes!),c.signal);this.active(c.signal);if(!(bytes instanceof Uint8Array)||bytes.byteLength!==Math.min(r.partBytes!,r.size-start))throw new Error("part size mismatch");const etag=await this.api.put(link.url,new Uint8Array(bytes),c.signal);this.active(c.signal);if(typeof etag!=="string"||!etag||etag.length>256||/[\r\n\x00]/.test(etag))throw new Error("invalid part etag");return{part_no:link.part_no,etag}}));this.active(c.signal);const failed=settled.find((result):result is PromiseRejectedResult=>result.status==="rejected");if(failed)throw failed.reason;const uploaded=settled.map(result=>{if(result.status!=="fulfilled")throw new Error("UPLOAD_BATCH_FAILED");return result.value});r.parts.push(...uploaded);r.parts.sort((a,b)=>a.part_no-b.part_no);await this.persist()}this.active(c.signal);if(s.verify){const digest=await s.verify(c.signal);this.active(c.signal);if(digest!==r.sha256)throw new Error("UPLOAD_FILE_MISMATCH")}if(checkedParts(r).length!==r.partsTotal)throw new Error("UPLOAD_CHECKPOINT_INCOMPLETE");r.state="completing";await this.persist();this.active(c.signal);await this.api.complete(r.uploadId,checkedParts(r),r.size,c.signal);this.active(c.signal);if(this.api.status){const remote=await this.current(r,c);if(remote==="uploading"||remote==="initializing")throw new Error("UPLOAD_COMPLETION_UNCONFIRMED");this.remote(r,remote)}else r.state="complete";this.sources.delete(r.localId);await this.persist()}catch(e){if(c.signal.aborted){if(r.state!=="aborted"&&r.state!=="queued"&&r.state!=="error")r.state="paused"}else{r.state="error";r.error=e instanceof Error?e.message:"UPLOAD_FAILED"}await this.persist()}}
  private require(id:string){const r=this.records.find(x=>x.localId===id);if(!r)throw new Error("upload not found");return r}
  private async persist(){
    if(this.closed)return;
    const snapshot=this.records.map(copyUploadRecord);
    const saving=this.saves.then(async()=>{if(!this.closed)await this.persistence.save(snapshot)});
    this.saves=saving.catch(()=>{});
    await saving;if(this.closed)return;for(const listener of this.listeners)listener();
  }
}
