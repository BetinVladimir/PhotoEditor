import {expect,it} from 'vitest';
import {ExportBatchAttemptStore,readExportBatchTicket,type ExportBatchAttempt} from './export-batch-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
it('preserves accepted groups while recovering missing packages and refuses regression',async()=>{const map=new Map<string,string>(),storage={getItem:async(k:string)=>map.get(k)??null,setItem:async(k:string,v:string)=>{map.set(k,v)},removeItem:async(k:string)=>{map.delete(k)}};const store=new ExportBatchAttemptStore(storage,'https://example.test',id(1),id(2),id(3),async(_origin,action)=>action());const request={snapshot_id:id(3),presets:[id(4),id(5)],approved_fields:[]};const old:ExportBatchAttempt={key:id(6),request,ticket:{batch_id:id(7),project_id:id(16),state:'partial',groups:[{key:id(8),package_id:id(9),metadata_policy:'strip',presets:[id(4)],failure_code:null,ticket:{run_id:id(10),workflow_id:id(11),state:'queued'}},{key:id(12),package_id:id(13),metadata_policy:'copyright',presets:[id(5)],failure_code:null,ticket:null}]}};await store.write(old);const next=structuredClone(old);next.ticket!.state='queued';next.ticket!.groups[1].ticket={run_id:id(14),workflow_id:id(15),state:'queued'};await store.write(next);await expect(async()=>store.write(old)).rejects.toThrow();await expect(async()=>store.write({...next,request:{...request,presets:[id(4)]}})).rejects.toThrow();expect((await store.read())?.ticket?.groups[0].ticket?.run_id).toBe(id(10))});
it('refuses missing preset groups and forged completion',()=>{const request={snapshot_id:id(3),presets:[id(4),id(5)]};expect(()=>readExportBatchTicket({batch_id:id(7),project_id:id(16),state:'queued',groups:[{key:id(8),package_id:id(9),metadata_policy:'strip',presets:[id(4)],failure_code:null,ticket:null}]},request)).toThrow()});

it('keeps accepted packages and permanent group failures immutable across recovery',async()=>{
 const map=new Map<string,string>(),storage={getItem:async(k:string)=>map.get(k)??null,setItem:async(k:string,v:string)=>{map.set(k,v)},removeItem:async(k:string)=>{map.delete(k)}};
 const store=new ExportBatchAttemptStore(storage,'https://terminal.example.test',id(1),id(2),id(3),async(_origin,action)=>action());
 const accepted:ExportBatchAttempt={key:id(6),request:{snapshot_id:id(3),presets:[id(4),id(5)]},ticket:{batch_id:id(7),project_id:id(16),state:'failed',groups:[
  {key:id(8),package_id:id(9),metadata_policy:'strip',presets:[id(4)],failure_code:null,ticket:{run_id:id(10),workflow_id:id(11),state:'queued'}},
  {key:id(12),package_id:id(13),metadata_policy:'copyright',presets:[id(5)],failure_code:'SNAPSHOT_EXPIRED',ticket:null}
 ]}};
 await store.write(accepted);
 const changedFailure=structuredClone(accepted);changedFailure.ticket!.groups[1].failure_code='NOT_FOUND';
 await expect(async()=>store.write(changedFailure)).rejects.toThrow('EXPORT_BATCH_JOURNAL_CONFLICT');
 const revived=structuredClone(accepted);revived.ticket!.state='queued';revived.ticket!.groups[1].failure_code=null;revived.ticket!.groups[1].ticket={run_id:id(14),workflow_id:id(15),state:'queued'};
 await expect(async()=>store.write(revived)).rejects.toThrow('EXPORT_BATCH_JOURNAL_CONFLICT');
 const substituted=structuredClone(accepted);substituted.ticket!.groups[0].ticket!.run_id=id(17);
 await expect(async()=>store.write(substituted)).rejects.toThrow('EXPORT_BATCH_JOURNAL_CONFLICT');
 expect(await store.read()).toEqual(accepted);
 await store.clearAccepted(accepted.key);expect(await store.read()).toBeNull();
});
it('refuses a group that claims both an accepted package and a permanent failure',()=>{
 const request={snapshot_id:id(3),presets:[id(4)]};
 expect(()=>readExportBatchTicket({batch_id:id(7),project_id:id(16),state:'failed',groups:[{key:id(8),package_id:id(9),metadata_policy:'strip',presets:[id(4)],failure_code:'SNAPSHOT_EXPIRED',ticket:{run_id:id(10),workflow_id:id(11),state:'queued'}}]},request)).toThrow('EXPORT_BATCH_TICKET_INVALID');
});

it('does not acknowledge a batch with an unresolved group or erase another scope',async()=>{
 const map=new Map<string,string>(),storage={getItem:async(k:string)=>map.get(k)??null,setItem:async(k:string,v:string)=>{map.set(k,v)},removeItem:async(k:string)=>{map.delete(k)}};
 const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
 const first=new ExportBatchAttemptStore(storage,'https://scopes.example.test',id(1),id(2),id(3),lock);
 const other=new ExportBatchAttemptStore(storage,'https://scopes.example.test',id(1),id(20),id(21),lock);
 const pending:ExportBatchAttempt={key:id(6),request:{snapshot_id:id(3),presets:[id(4)]},ticket:{batch_id:id(7),project_id:id(16),state:'partial',groups:[{key:id(8),package_id:id(9),metadata_policy:'strip',presets:[id(4)],failure_code:null,ticket:null}]}};
 const separate:ExportBatchAttempt={key:id(22),request:{snapshot_id:id(21),presets:[id(4)]},ticket:null};
 await first.write(pending);await other.write(separate);
 await expect(first.clearAccepted(pending.key)).rejects.toThrow('EXPORT_BATCH_JOURNAL_CONFLICT');
 expect(await first.read()).toEqual(pending);expect(await other.read()).toEqual(separate);
});
