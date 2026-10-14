import {expect,it} from 'vitest';
import {DeliveryCreateAttemptStore,clearDeliveryCreateAttempts,type DeliveryCreateAttempt} from './delivery-create-attempt-store';
import type {SemanticJournalLock} from './semantic-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock:SemanticJournalLock=async(_origin,action)=>action();
function setup(){const data=new Map<string,string>();return {getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};}
function attempt():DeliveryCreateAttempt{return {version:1,key:id(8),body:{project_id:id(3),snapshot_id:id(4),name:'Delivery',policy:{required_presets:[id(5)],optional_presets:[],metadata_policy:'strip',provenance:'off',require_review:true,allow_omitted:false}},ticket:null};}
it('restores immutable intent across mounts and owns returned values',async()=>{
 const storage=setup(),first=new DeliveryCreateAttemptStore(storage,'origin',id(1),id(2),id(3),id(4),lock),record=attempt();await first.write(record);
 record.body.name='mutated';const second=new DeliveryCreateAttemptStore(storage,'origin',id(1),id(2),id(3),id(4),lock);
 const saved=await second.read();expect(saved?.body.name).toBe('Delivery');saved!.body.name='changed';expect((await second.read())?.body.name).toBe('Delivery');
});
it('refuses a concurrent replacement and ticket regression',async()=>{
 const storage=setup(),store=new DeliveryCreateAttemptStore(storage,'origin',id(1),id(2),id(3),id(4),lock),record=attempt();await store.write(record);
 await expect(store.write({...record,key:id(9)})).rejects.toThrow('CONFLICT');
 const accepted={...record,ticket:{run_id:id(10),workflow_id:id(11),state:'queued' as const}};await store.write(accepted);
 await expect(store.write(record)).rejects.toThrow('CONFLICT');expect(await store.read()).toEqual(accepted);
});
it('isolates users and invalidates mounted writers after logout',async()=>{
 const storage=setup(),store=new DeliveryCreateAttemptStore(storage,'origin',id(1),id(2),id(3),id(4),lock);await store.write(attempt());
 expect(await new DeliveryCreateAttemptStore(storage,'origin',id(9),id(2),id(3),id(4),lock).read()).toBeNull();
 await clearDeliveryCreateAttempts(storage,'origin',lock);await expect(store.write(attempt())).rejects.toThrow('EXPIRED');
 expect(await new DeliveryCreateAttemptStore(storage,'origin',id(1),id(2),id(3),id(4),lock).read()).toBeNull();
});
it('refuses discarding an unacknowledged attempt',async()=>{
 const storage=setup(),store=new DeliveryCreateAttemptStore(storage,'other-origin',id(1),id(2),id(3),id(4),lock);await store.write(attempt());
 await expect(store.clear(id(8))).rejects.toThrow('CONFLICT');expect(await store.read()).not.toBeNull();
});
it('releases only the exact acknowledged ticket for another create on the same snapshot',async()=>{
 const storage=setup(),store=new DeliveryCreateAttemptStore(storage,'repeat-origin',id(1),id(2),id(3),id(4),lock);
 const pending=attempt(),ticket={run_id:id(10),workflow_id:id(11),state:'queued' as const};
 await store.write(pending);
 await expect(store.clearAccepted(ticket)).rejects.toThrow('CONFLICT');
 await store.write({...pending,ticket});
 await expect(store.clearAccepted({...ticket,run_id:id(12)})).rejects.toThrow('CONFLICT');
 expect((await store.read())?.ticket).toEqual(ticket);
 const callerTicket={...ticket},clearing=store.clearAccepted(callerTicket);
 callerTicket.run_id=id(12);
 await clearing;expect(await store.read()).toBeNull();
 await store.write({...pending,key:id(9)});expect((await store.read())?.key).toBe(id(9));
});

it('checks exact journal installation scope without reading storage',()=>{
 const storage={getItem:async()=>{throw new Error('unexpected IO')},setItem:async()=>{},removeItem:async()=>{}};
 const store=new DeliveryCreateAttemptStore(storage,'scope-origin',id(1),id(2),id(3),id(4),lock);
 expect(()=>store.assertScope(id(2),id(3),id(4))).not.toThrow();
 for(const scope of [[id(90),id(3),id(4)],[id(2),id(90),id(4)],[id(2),id(3),id(90)]]){
  expect(()=>store.assertScope(scope[0],scope[1],scope[2])).toThrow('DELIVERY_CREATE_JOURNAL_SCOPE_INVALID');
 }
});
