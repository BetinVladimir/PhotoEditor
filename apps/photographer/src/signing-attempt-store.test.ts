import {expect,it,vi} from 'vitest';
import {SigningAttemptStore,clearSigningAttempts} from './signing-attempt-store';
import {submitSigning} from './signing-submit';
import type {SemanticJournalLock} from './semantic-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock:SemanticJournalLock=async(_origin,action)=>action();
function storage(){const data=new Map<string,string>();return {getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};}
const ticket=()=>({run_id:id(8),workflow_id:id(9),state:'queued' as const});

it('persists before network and restores the same key across mounts after ambiguity',async()=>{
 const data=storage(),first=new SigningAttemptStore(data,'origin-a',id(1),id(2),id(3),lock);
 const api={signExport:vi.fn(async()=>{expect((await first.read())?.key).toBe('original-key');throw new Error('network')})};
 await expect(submitSigning(first,api,{signing_policy_id:id(4)},'original-key')).rejects.toThrow('network');
 const second=new SigningAttemptStore(data,'origin-a',id(1),id(2),id(3),lock),success={signExport:vi.fn().mockResolvedValue(ticket())};
 expect(await submitSigning(second,success,{signing_policy_id:id(4)},'new-key-not-used')).toEqual(ticket());
 expect(success.signExport).toHaveBeenCalledWith(id(3),{signing_policy_id:id(4)},'original-key',undefined);
 await submitSigning(second,success,{signing_policy_id:id(4)},'another-unused-key');
 expect(success.signExport).toHaveBeenCalledTimes(1);
});

it('isolates scope, rejects replacement and ticket regression, owns returned values',async()=>{
 const data=storage(),store=new SigningAttemptStore(data,'origin-b',id(1),id(2),id(3),lock);
 const attempt={version:1 as const,key:'key',body:{signing_policy_id:id(4)},ticket:null};await store.write(attempt);
 attempt.body.signing_policy_id=id(5);expect((await store.read())?.body.signing_policy_id).toBe(id(4));
 const saved=(await store.read())!;await expect(store.write({...saved,key:'replacement'})).rejects.toThrow('CONFLICT');
 const api={signExport:vi.fn()};await expect(submitSigning(store,api,{signing_policy_id:id(5)},'new')).rejects.toThrow('CONFLICT');expect(api.signExport).not.toHaveBeenCalled();
 expect(await new SigningAttemptStore(data,'origin-b',id(6),id(2),id(3),lock).read()).toBeNull();
 expect(await new SigningAttemptStore(data,'origin-b',id(1),id(7),id(3),lock).read()).toBeNull();
 expect(await new SigningAttemptStore(data,'origin-b',id(1),id(2),id(7),lock).read()).toBeNull();
 await expect(store.clear('key')).rejects.toThrow('CONFLICT');
 await store.write({...saved,ticket:ticket()});await expect(store.write(saved)).rejects.toThrow('CONFLICT');
});

it('logout invalidates mounted writers and removes persisted attempts',async()=>{
 const data=storage(),store=new SigningAttemptStore(data,'origin-c',id(1),id(2),id(3),lock);
 const record={version:1 as const,key:'key',body:{signing_policy_id:id(4)},ticket:null};await store.write(record);
 await clearSigningAttempts(data,'origin-c',lock);await expect(store.write(record)).rejects.toThrow('EXPIRED');
 expect(await new SigningAttemptStore(data,'origin-c',id(1),id(2),id(3),lock).read()).toBeNull();
});

it('refuses network when persistence fails',async()=>{
 const data={...storage(),setItem:async()=>{throw new Error('storage unavailable')}},store=new SigningAttemptStore(data,'origin-d',id(1),id(2),id(3),lock);
 const api={signExport:vi.fn()};await expect(submitSigning(store,api,{signing_policy_id:id(4)},'key')).rejects.toThrow('storage unavailable');expect(api.signExport).not.toHaveBeenCalled();
});

it('keeps the pending original key when cancellation arrives with the server response',async()=>{
 const data=storage(),store=new SigningAttemptStore(data,'origin-cancel',id(1),id(2),id(3),lock),controller=new AbortController();
 const api={signExport:vi.fn(async()=>{controller.abort();return ticket()})};
 await expect(submitSigning(store,api,{signing_policy_id:id(4)},'original',controller.signal)).rejects.toThrow();
 expect(await store.read()).toEqual({version:1,key:'original',body:{signing_policy_id:id(4)},ticket:null});
 const retry={signExport:vi.fn().mockResolvedValue(ticket())};
 await submitSigning(store,retry,{signing_policy_id:id(4)},'ignored-replacement');
 expect(retry.signExport).toHaveBeenCalledWith(id(3),{signing_policy_id:id(4)},'original',undefined);
});

it('retains pending intent when storing an acknowledged ticket fails',async()=>{
 const base=storage();let fail=true;
 const data={...base,setItem:async(key:string,value:string)=>{
  if(fail&&JSON.parse(value).ticket!==null)throw new Error('ticket storage failed');
  return base.setItem(key,value);
 }};
 const store=new SigningAttemptStore(data,'origin-ticket-failure',id(1),id(2),id(3),lock),api={signExport:vi.fn().mockResolvedValue(ticket())};
 await expect(submitSigning(store,api,{signing_policy_id:id(4)},'retained')).rejects.toThrow('ticket storage failed');
 expect((await store.read())?.ticket).toBeNull();expect((await store.read())?.key).toBe('retained');
 fail=false;await submitSigning(store,api,{signing_policy_id:id(4)},'unused');
 expect(api.signExport.mock.calls.map(call=>call[2])).toEqual(['retained','retained']);
});

it('retains saved acknowledgement if cancellation arrives after ticket persistence',async()=>{
 const base=storage(),controller=new AbortController();
 const data={...base,setItem:async(key:string,value:string)=>{
  await base.setItem(key,value);
  if(JSON.parse(value).ticket!==null)controller.abort();
 }};
 const store=new SigningAttemptStore(data,'origin-after-persist',id(1),id(2),id(3),lock),api={signExport:vi.fn().mockResolvedValue(ticket())};
 await expect(submitSigning(store,api,{signing_policy_id:id(4)},'retained',controller.signal)).rejects.toThrow();
 expect((await store.read())?.ticket).toEqual(ticket());
 await submitSigning(store,api,{signing_policy_id:id(4)},'unused');expect(api.signExport).toHaveBeenCalledTimes(1);
});

it('does not repopulate logout-cleared storage from an in-flight response',async()=>{
 const data=storage(),store=new SigningAttemptStore(data,'origin-inflight-logout',id(1),id(2),id(3),lock);
 const api={signExport:vi.fn(async()=>{await clearSigningAttempts(data,'origin-inflight-logout',lock);return ticket()})};
 await expect(submitSigning(store,api,{signing_policy_id:id(4)},'old-session')).rejects.toThrow('EXPIRED');
 expect(await new SigningAttemptStore(data,'origin-inflight-logout',id(1),id(2),id(3),lock).read()).toBeNull();
});

it('owns the API callback request so mutation cannot replace the persisted policy',async()=>{
 const data=storage(),store=new SigningAttemptStore(data,'origin-callback',id(1),id(2),id(3),lock);
 const api={signExport:vi.fn(async(_export:string,body:{signing_policy_id:string})=>{body.signing_policy_id=id(7);return ticket()})};
 await submitSigning(store,api,{signing_policy_id:id(4)},'key');
 expect((await store.read())?.body.signing_policy_id).toBe(id(4));
});
