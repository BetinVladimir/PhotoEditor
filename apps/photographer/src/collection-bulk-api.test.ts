import {describe,it,expect} from 'vitest';
import {CollectionBulkAPI,freezeBulkCommand,readBulkReport,readBulkTicket} from './collection-bulk-api';
const id=(n:number)=>`00000000-0000-0000-0000-${String(n).padStart(12,'0')}`;
const command={revision:'9',item_ids:[id(1),id(2)],action:'rating' as const,params:{rating:5}};
const ticket={bulk_operation_id:id(3),run_id:id(4),status:'queued'};
const report={id:id(3),collection_id:id(5),snapshot_id:id(6),run_id:id(4),action:'rating',status:'completed_with_errors',cancel_requested:false,expected_next_revision:'10',counts:{total:2,succeeded:1,failed:1,skipped:0},results:{next_cursor:null,items:[{item_id:id(1),ordinal:0,status:'succeeded',result:{}},{item_id:id(2),ordinal:1,status:'failed',error_code:'REVISION_CONFLICT',result:null}]}};
const expected={collection:id(5),snapshot:id(6),ticket,command};
describe('actual collection bulk wire contract',()=>{
 it('freezes all actually supported parameter families',()=>{for(const [action,params] of [['rating',{rating:0}],['flag',{flag:'reject'}],['tag',{tag:'client',mode:'remove'}],['hide',{}],['publish',{publish:false}],['copy',{target_collection_id:id(8)}],['move',{target_collection_id:id(8)}],['remove',{}],['retry_media',{variants:['thumbnail','preview']}]] as const){expect(freezeBulkCommand({...command,action,params}).action).toBe(action)}});
 it('refuses inexact revisions, duplicate selection, injected and invalid action params',()=>{for(const value of [{...command,revision:'9007199254740993'},{...command,item_ids:[id(1),id(1)]},{...command,params:{rating:6}},{...command,params:{rating:5,extra:true}}])expect(()=>freezeBulkCommand(value)).toThrow()});
 it('requires exact acceptance identities',()=>{expect(readBulkTicket(ticket)).toEqual(ticket);expect(()=>readBulkTicket({...ticket,run_id:'invalid'})).toThrow()});
 it('binds report to persisted command and exact server item ids',()=>{expect(readBulkReport(report,expected)).toEqual(report);for(const value of [{...report,collection_id:id(9)},{...report,snapshot_id:id(9)},{...report,run_id:id(9)},{...report,action:'remove'},{...report,counts:{...report.counts,total:3}},{...report,results:{items:[{ordinal:0,status:'succeeded',result:{}}]}},{...report,results:{items:[{item_id:id(9),ordinal:0,status:'succeeded',result:{}}]}}])expect(()=>readBulkReport(value,expected)).toThrow()});
});

it('combined abandon requires exact parent/key or the complete accepted snapshot-ticket pair',async()=>{
 const controller=new AbortController(),snapshot={id:id(6),digest:'a'.repeat(64),item_count:2,expires_at:'2099-01-01T00:00:00Z'};
 let response:unknown={status:'accepted',snapshot,ticket};const calls:unknown[]=[];const api=new CollectionBulkAPI({request:async(path:string,options:unknown)=>{calls.push({path,options});return response}} as never,id(7),id(5));
 expect(await api.abandon(command,id(10),id(11),controller.signal)).toEqual(response);
 for(const bad of [{status:'accepted',ticket},{status:'accepted',snapshot:{...snapshot,item_count:3},ticket},{status:'accepted',snapshot,ticket:{...ticket,run_id:'invalid'}},{status:'abandoned',key:id(12),collection_id:id(5)},{status:'abandoned',key:id(11),collection_id:id(12)}]){response=bad;await expect(api.abandon(command,id(10),id(11),controller.signal)).rejects.toThrow('BULK_RESPONSE_INVALID')}
 response={status:'abandoned',key:id(11),collection_id:id(5)};expect(await api.abandon(command,id(10),id(11),controller.signal)).toEqual(response);
 expect(calls[0]).toMatchObject({options:{idempotencyKey:id(11),body:{selection_key:id(10),selection:{collection_revision:'9',scope:'selected',item_ids:command.item_ids},action:'rating',params:{rating:5}}}});
});


it('rejects missing and expanded report fields and returns a detached report',()=>{
 for(const value of [{...report,extra:true},{...report,counts:{...report.counts,extra:1}},{...report,results:{items:report.results.items}},{...report,results:{...report.results,revision:'1'}},{...report,results:{next_cursor:null,items:[{...report.results.items[0],extra:true}]}}])expect(()=>readBulkReport(value,expected)).toThrow('BULK_RESPONSE_INVALID');
 const decoded=readBulkReport(report,expected);decoded.counts.failed=0;decoded.results.items[0].item_id=id(99);expect(report.counts.failed).toBe(1);expect(report.results.items[0].item_id).toBe(id(1));
});
it('does not send any Bulk request on an already stopped lifetime',async()=>{
 let calls=0;const api=new CollectionBulkAPI({request:async()=>{calls++;throw new Error('transport must not run')}} as never,id(7),id(5));const c=new AbortController();c.abort();
 for(const call of [()=>api.actor(c.signal),()=>api.destinations(id(8),c.signal),()=>api.snapshot(command,id(10),c.signal),()=>api.start(command,id(6),id(11),c.signal),()=>api.abandon(command,id(10),id(11),c.signal),()=>api.report(command,id(6),ticket,c.signal),()=>api.report(command,id(6),ticket,c.signal,undefined,true)])await expect(call()).rejects.toThrow();expect(calls).toBe(0);
});
it('freezes selection and action before transport and response validation',async()=>{
 const local={...command,item_ids:[...command.item_ids],params:{rating:5}};let finish!:(v:unknown)=>void;let sent:unknown;
 const api=new CollectionBulkAPI({request:async(_path:string,options:unknown)=>{sent=options;return new Promise(resolve=>{finish=resolve})}} as never,id(7),id(5));
 const pending=api.snapshot(local,id(10),new AbortController().signal);local.item_ids.push(id(9));local.params.rating=0;
 finish({id:id(6),digest:'a'.repeat(64),item_count:2,expires_at:'2099-01-01T00:00:00Z'});expect((await pending).item_count).toBe(2);expect(sent).toMatchObject({body:{item_ids:[id(1),id(2)]}});
});
it('rejects invalid command, snapshot, ticket, cursor and key before transport',async()=>{
 let calls=0;const api=new CollectionBulkAPI({request:async()=>{calls++;return ticket}} as never,id(7),id(5));const signal=new AbortController().signal;
 for(const call of [()=>api.snapshot(command,'',signal),()=>api.start(command,'bad',id(11),signal),()=>api.start({...command,revision:'9007199254740993'},id(6),id(11),signal),()=>api.abandon(command,'',id(11),signal),()=>api.report(command,id(6),{...ticket,run_id:'bad'},signal),()=>api.report(command,id(6),ticket,signal,'')])await expect(call()).rejects.toThrow('BULK_RESPONSE_INVALID');expect(calls).toBe(0);
});

it('accepts signed-int64 report revisions without unsafe numeric conversion',()=>{
 expect(readBulkReport({...report,expected_next_revision:'9223372036854775807'},expected).expected_next_revision).toBe('9223372036854775807');
 for(const value of ['9223372036854775808','01','0'])expect(()=>readBulkReport({...report,expected_next_revision:value},expected)).toThrow();
});
