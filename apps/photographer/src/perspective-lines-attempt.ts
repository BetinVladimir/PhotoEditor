import {freezePerspectiveLinesIntent,readPerspectiveLinesReceipt,type PerspectiveLinesIntent,type PerspectiveLinesReceipt} from './perspective-lines';
export type PerspectiveLinesAttemptRecord={schema_version:1;tenant:string;project:string;key:string;body:PerspectiveLinesIntent;receipt:PerspectiveLinesReceipt|null};
export type PerspectiveLinesPersistence={write:(record:PerspectiveLinesAttemptRecord)=>Promise<void>};
export class PerspectiveLinesAttempt{
 private record:PerspectiveLinesAttemptRecord;
 private phase:'ready'|'sending'|'unknown'|'accepted'='ready';
 constructor(tenant:string,project:string,key:string,body:PerspectiveLinesIntent,private persistence:PerspectiveLinesPersistence){
 const uuid=(v:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
 if(!uuid(tenant)||!uuid(project)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('PERSPECTIVE_LINES_ATTEMPT_INVALID');
 this.record={schema_version:1,tenant,project,key,body:freezePerspectiveLinesIntent(body),receipt:null};
 }
 get state(){return this.phase;}
 get snapshot(){return structuredClone(this.record);}
 static restore(record:PerspectiveLinesAttemptRecord,persistence:PerspectiveLinesPersistence){
 if(!record||record.schema_version!==1||Object.keys(record).sort().join(',')!=='body,key,project,receipt,schema_version,tenant')throw new Error('PERSPECTIVE_LINES_ATTEMPT_INVALID');
 const attempt=new PerspectiveLinesAttempt(record.tenant,record.project,record.key,record.body,persistence);
 if(record.receipt!==null){attempt.record.receipt=readPerspectiveLinesReceipt(record.receipt,record.tenant,record.project,record.body);attempt.phase='accepted';}else attempt.phase='unknown';
 return attempt;
 }
 async submit(send:(project:string,body:PerspectiveLinesIntent,key:string,signal?:AbortSignal)=>Promise<PerspectiveLinesReceipt>,signal?:AbortSignal){
 if(this.phase==='sending')throw new Error('PERSPECTIVE_LINES_ATTEMPT_BUSY');
 if(signal?.aborted)throw new Error('PERSPECTIVE_LINES_ATTEMPT_CANCELLED');
 if(this.record.receipt)return structuredClone(this.record.receipt);
 this.phase='sending';
 try{
 await this.persistence.write(this.snapshot);
 if(signal?.aborted)throw new Error('PERSPECTIVE_LINES_ATTEMPT_CANCELLED');
 const receipt=readPerspectiveLinesReceipt(await send(this.record.project,freezePerspectiveLinesIntent(this.record.body),this.record.key,signal),this.record.tenant,this.record.project,this.record.body);
 await this.persistence.write({...this.snapshot,receipt:structuredClone(receipt)});
 this.record.receipt=receipt;this.phase='accepted';return structuredClone(receipt);
 }catch(error){this.phase='unknown';throw error;}
 }
}
