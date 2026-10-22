import type {EditorDraft,MaskRef,RunAccepted} from './catalog-types';
export type EditorGenerationAttemptRecord={version:1;key:string;input:{draft:EditorDraft;mask:MaskRef;prompt:string;seed:number};receipt:RunAccepted|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const sha=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
function maskRef(v:unknown):v is MaskRef{
 return object(v)&&object(v.artifact)&&uuid(v.artifact.artifact_id)&&sha(v.artifact.sha256)
  &&object(v.canvas)&&Number.isInteger(v.canvas.width)&&Number(v.canvas.width)>0&&Number(v.canvas.width)<=100000
  &&Number.isInteger(v.canvas.height)&&Number(v.canvas.height)>0&&Number(v.canvas.height)<=100000
  &&object(v.transform_chain)&&typeof v.selector==='string'&&v.selector.length>0&&v.selector.length<=256
  &&typeof v.confidence==='number'&&Number.isFinite(v.confidence)&&v.confidence>=0&&v.confidence<=1&&typeof v.protected==='boolean';
}
export function readEditorGenerationAttempt(raw:string):EditorGenerationAttemptRecord{
 const fail=():never=>{throw new Error('GENERATION_JOURNAL_INVALID')};
 if(raw.length>1048576||new TextEncoder().encode(raw).byteLength>1048576)return fail();
 let value:unknown;try{value=JSON.parse(raw)}catch{return fail()}
 if(!object(value)||Object.keys(value).sort().join(',')!=='input,key,receipt,version'||value.version!==1||!uuid(value.key)||!object(value.input)||Object.keys(value.input).sort().join(',')!=='draft,mask,prompt,seed')return fail();
 const input=value.input,draft=input.draft;
 if(!object(draft)||!uuid(draft.asset_version_id)||typeof draft.revision!=='string'||!/^[1-9][0-9]{0,15}$/.test(draft.revision)||BigInt(draft.revision)>9007199254740991n||!sha(draft.source_sha256)||!object(draft.params)||!Array.isArray(draft.mask_refs)||draft.mask_refs.length>1024||!draft.mask_refs.every(maskRef)||!maskRef(input.mask)||typeof input.prompt!=='string'||!input.prompt.trim()||input.prompt.length>8192||!Number.isInteger(input.seed)||Number(input.seed)<0||Number(input.seed)>4294967295)return fail();
 if(value.receipt!==null&&(!object(value.receipt)||Object.keys(value.receipt).sort().join(',')!=='run_id,status'||!uuid(value.receipt.run_id)||typeof value.receipt.status!=='string'||!/^([a-z_]{1,64})$/.test(value.receipt.status)))return fail();
 return structuredClone(value) as EditorGenerationAttemptRecord;
}
type Send=(draft:EditorDraft,mask:MaskRef,prompt:string,seed:number,count:number,key:string)=>Promise<RunAccepted>;
export class EditorGenerationAttempt {
 readonly key:string;
 private input:{draft:EditorDraft;mask:MaskRef;prompt:string;seed:number};
 private receipt:RunAccepted|null=null;
 private flight:Promise<RunAccepted>|null=null;
 private journal:{write:(record:EditorGenerationAttemptRecord)=>Promise<void>}|null=null;
 attach(journal:{write:(record:EditorGenerationAttemptRecord)=>Promise<void>}){this.journal=journal;return this;}
 constructor(draft:EditorDraft,mask:MaskRef,prompt:string,seed:number,key:string=crypto.randomUUID()){this.key=key;this.input=structuredClone({draft,mask,prompt,seed});}
 get accepted():RunAccepted|null{return this.receipt?structuredClone(this.receipt):null;}
 get snapshot():EditorGenerationAttemptRecord{return structuredClone({version:1 as const,key:this.key,input:this.input,receipt:this.receipt});}
 static restore(raw:string):EditorGenerationAttempt{
  const value=readEditorGenerationAttempt(raw),{draft,mask,prompt,seed}=value.input;
  const attempt=new EditorGenerationAttempt(draft,mask,prompt,seed,value.key);attempt.receipt=value.receipt;return attempt;
 }
 submit(send:Send):Promise<RunAccepted>{
  if(this.flight)return this.flight.then(value=>structuredClone(value));
  const input=structuredClone(this.input);
  this.flight=(async()=>{
   if(this.journal)await this.journal.write(this.snapshot);
   if(this.receipt)return structuredClone(this.receipt);
   return send(input.draft,input.mask,input.prompt,input.seed,1,this.key);
  })().then(async value=>{
   if(!object(value)||Object.keys(value).sort().join(',')!=='run_id,status'||!uuid(value.run_id)||typeof value.status!=='string'||!/^([a-z_]{1,64})$/.test(value.status))throw new Error('GENERATION_RECEIPT_INVALID');
   this.receipt=structuredClone(value);if(this.journal)await this.journal.write(this.snapshot);return structuredClone(value);
  }).finally(()=>{this.flight=null});
  return this.flight;
 }
}
