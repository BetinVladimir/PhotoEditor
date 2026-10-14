import {expect,it,vi} from 'vitest';
import {executeInterchangeCreate,openInterchangeCreate} from './interchange-create-controller';
import {InterchangeCreateAttemptStore} from './interchange-create-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_key:string,action:()=>Promise<T>)=>action();
function setup(){
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={project_id:id(2),direction:'import' as const,format:'photoeditor_bundle' as const,bundle:{artifact_id:id(3),sha256:'a'.repeat(64)}};
 const journal=new InterchangeCreateAttemptStore(storage,'https://create-controller.test',id(9),id(1),request,lock);
 const ticket={run_id:id(4),workflow_id:id(5),state:'queued' as const};
 return {values,journal,request,ticket};
}
it('serializes concurrent creates and reuses acknowledged ticket',async()=>{
 const f=setup(),newKey=vi.fn(()=>'original');
 const api={createInterchange:vi.fn(async()=>{expect((await f.journal.read())?.key).toBe('original');return f.ticket;})};
 const options={api,journal:f.journal,tenant:id(1),request:f.request,signal:new AbortController().signal,newKey,lock};
 const [a,b]=await Promise.all([executeInterchangeCreate(options),executeInterchangeCreate(options)]);
 expect(a.ticket).toEqual(f.ticket);expect(b).toEqual(a);expect(api.createInterchange).toHaveBeenCalledOnce();expect(newKey).toHaveBeenCalledOnce();
});
it('lost response retains original key and callback cannot mutate persisted request',async()=>{
 const f=setup(),newKey=vi.fn(()=>'retained');
 const api={createInterchange:vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce(f.ticket)};
 const options={api,journal:f.journal,tenant:id(1),request:f.request,signal:new AbortController().signal,newKey,lock,onAttempt:(record:{key:string})=>{record.key='changed';}};
 await expect(executeInterchangeCreate(options)).rejects.toThrow('lost');
 expect((await f.journal.read())?.key).toBe('retained');
 await executeInterchangeCreate(options);
 expect(api.createInterchange.mock.calls.map(call=>call[1])).toEqual(['retained','retained']);expect(newKey).toHaveBeenCalledOnce();
});
it('deleted durable attempt after response refuses ticket resurrection',async()=>{
 const f=setup();
 const api={createInterchange:vi.fn(async()=>{f.values.delete(f.journal.key);return f.ticket;})};
 await expect(executeInterchangeCreate({api,journal:f.journal,tenant:id(1),request:f.request,signal:new AbortController().signal,newKey:()=> 'deleted',lock})).rejects.toThrow('INTERCHANGE_CREATE_JOURNAL_CONFLICT');
 expect(await f.journal.read()).toBeNull();
});


it.each(['deleted','replaced'])('stale retry refuses %s intent without generating a new operation',async mode=>{
 const f=setup();
 if(mode==='replaced')await f.journal.write({version:1,key:'another-operation',body:f.request,ticket:null});
 const api={createInterchange:vi.fn()},newKey=vi.fn(()=>'must-not-create');
 await expect(executeInterchangeCreate({api,journal:f.journal,tenant:id(1),request:f.request,signal:new AbortController().signal,expectedAttemptKey:'original-operation',newKey,lock})).rejects.toThrow('INTERCHANGE_CREATE_JOURNAL_CONFLICT');
 expect(newKey).not.toHaveBeenCalled();expect(api.createInterchange).not.toHaveBeenCalled();
});

it('cancelled queued create never allocates intent or calls transport',async()=>{
 const f=setup(),controller=new AbortController();controller.abort();
 const api={createInterchange:vi.fn()},newKey=vi.fn(()=>'cancelled');
 await expect(executeInterchangeCreate({api,journal:f.journal,tenant:id(1),request:f.request,signal:controller.signal,newKey,lock})).rejects.toThrow();
 expect(newKey).not.toHaveBeenCalled();expect(api.createInterchange).not.toHaveBeenCalled();expect(await f.journal.read()).toBeNull();
});


it.each(['valid','deleted','foreign_ticket','cancelled'])('opens only exact durable acknowledged attempt: %s',async mode=>{
 const f=setup(),record={version:1 as const,key:'acknowledged',body:f.request,ticket:f.ticket};
 await f.journal.write(record);
 const expected={...record,ticket:{...f.ticket}};
 if(mode==='deleted')await f.journal.clear(record.key);
 if(mode==='foreign_ticket')expected.ticket.run_id=id(7);
 const controller=new AbortController();if(mode==='cancelled')controller.abort();
 let held=false;
 const executionLock=async<T>(_key:string,action:()=>Promise<T>)=>{held=true;try{return await action();}finally{held=false;}};
 const onCreated=vi.fn((ticket:{run_id:string})=>{expect(held).toBe(true);ticket.run_id=id(8);});
 const opening=openInterchangeCreate({journal:f.journal,tenant:id(1),request:f.request,expected,signal:controller.signal,onCreated,lock:executionLock});
 if(mode==='valid'){await opening;expect(onCreated).toHaveBeenCalledOnce();expect((await f.journal.read())?.ticket?.run_id).toBe(f.ticket.run_id);}else{await expect(opening).rejects.toThrow();expect(onCreated).not.toHaveBeenCalled();}
 expect(held).toBe(false);
});


it('acknowledgement callback cannot erase durable ticket and still return success',async()=>{
 const f=setup(),api={createInterchange:vi.fn(async()=>f.ticket)};
 await expect(executeInterchangeCreate({api,journal:f.journal,tenant:id(1),request:f.request,signal:new AbortController().signal,newKey:()=> 'ack-erased',lock,onAttempt:record=>{if(record.ticket)f.values.delete(f.journal.key);}})).rejects.toThrow('INTERCHANGE_CREATE_JOURNAL_CONFLICT');
 expect(api.createInterchange).toHaveBeenCalledOnce();expect(await f.journal.read()).toBeNull();
});
