import {expect,it} from 'vitest';
import {InterchangeCreateAttemptStore,clearInterchangeCreateAttempts,type InterchangeCreateAttempt} from './interchange-create-attempt-store';
import type {InterchangeCreateRequest} from './interchange-create';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
function body(direction:'import'|'export'):InterchangeCreateRequest{return direction==='import'?{project_id:id(3),direction,format:'photoeditor_bundle',bundle:{artifact_id:id(4),sha256:'a'.repeat(64)}}:{project_id:id(3),direction,format:'photoeditor_bundle',snapshot_id:id(4)};}
const ticket={run_id:id(5),workflow_id:id(6),state:'queued' as const};
it.each(['import','export'] as const)('restores immutable %s request/key/ticket and forbids regression',async direction=>{
 const data=storage(),request=body(direction),journal=new InterchangeCreateAttemptStore(data,'restore-origin',id(1),id(2),request,lock);
 const record:InterchangeCreateAttempt={version:1,key:'original',body:request,ticket:null};await journal.write(record);
 const another=new InterchangeCreateAttemptStore(data,'restore-origin',id(1),id(2),body(direction),lock);
 expect(await another.read()).toEqual(record);
 await expect(journal.clear('original')).rejects.toThrow('CONFLICT');
 await journal.write({...record,ticket});await expect(another.write(record)).rejects.toThrow('CONFLICT');
 await expect(another.write({...record,key:'replacement',ticket})).rejects.toThrow('CONFLICT');
 await expect(another.write({...record,ticket:{...ticket,run_id:id(90)}})).rejects.toThrow('CONFLICT');
 expect((await another.read())?.ticket).toEqual(ticket);
 await another.clear('original');expect(await journal.read()).toBeNull();
});
it('owns constructor scope and separates source digest, direction, format and principals',async()=>{
 const data=storage(),request=body('import'),journal=new InterchangeCreateAttemptStore(data,'scope-origin',id(1),id(2),request,lock);
 await journal.write({version:1,key:'original',body:request,ticket:null});
 const changed=body('import');if(changed.direction==='import')changed.bundle.sha256='b'.repeat(64);
 for(const different of [changed,body('export'),{...body('import'),format:'xmp' as const}]){
  expect(()=>journal.assertScope(id(2),different)).toThrow('SCOPE_INVALID');
  expect(await new InterchangeCreateAttemptStore(data,'scope-origin',id(1),id(2),different,lock).read()).toBeNull();
 }
 expect(await new InterchangeCreateAttemptStore(data,'scope-origin',id(90),id(2),request,lock).read()).toBeNull();
 if(request.direction==='import')request.bundle.sha256='c'.repeat(64);
 journal.assertScope(id(2),body('import'));
});
it('fences old writers and clears only matching origin on logout',async()=>{
 const data=storage(),one=new InterchangeCreateAttemptStore(data,'one',id(1),id(2),body('export'),lock),two=new InterchangeCreateAttemptStore(data,'two',id(1),id(2),body('export'),lock);
 const record={version:1 as const,key:'original',body:body('export'),ticket:null};await one.write(record);await two.write(record);
 await clearInterchangeCreateAttempts(data,'one',lock);
 await expect(one.write(record)).rejects.toThrow('EXPIRED');expect(await two.read()).toEqual(record);
 expect(await new InterchangeCreateAttemptStore(data,'one',id(1),id(2),body('export'),lock).read()).toBeNull();
});
