import {describe,it,expect} from 'vitest';
import {readCaptureAlertPage} from './capture-alert-response';
const id='12345678-1234-4234-8234-123456789abc';
const other='22345678-1234-4234-8234-123456789abc';
const alert=()=>({id,session_id:id,asset_version_id:id,report_artifact_id:id,
 receipt_id:null,confirmation_id:null,revision:'9007199254740993',severity:'warning',
 qa_stale:false,created_at:'2026-10-11T12:00:00Z',acknowledged_by:null,acknowledged_at:null});
describe('Capture alert response boundary',()=>{
 it('preserves exact bigint revision and owns each returned row',()=>{
  const row=alert();const page=readCaptureAlertPage({items:[row],next_cursor:null},id);
  expect(page.items[0].revision).toBe('9007199254740993');
  expect(page.items[0]).not.toBe(row);
 });
 it('rejects foreign scope, malformed state and inconsistent acknowledgement',()=>{
  for(const change of [{session_id:other},{id:'bad'},{receipt_id:'bad'},
   {confirmation_id:'bad'},{asset_version_id:null},{report_artifact_id:null},
   {revision:'01'},{revision:'0'},{revision:'9223372036854775808'},
   {revision:1},{qa_stale:'false'},{severity:'unknown'},
   {created_at:'bad'},{acknowledged_by:id},{acknowledged_at:'2026-10-11T12:00:00Z'}]){
   expect(()=>readCaptureAlertPage({items:[{...alert(),...change}],next_cursor:null},id)).toThrow();
  }
 });
 it('rejects duplicate rows and malformed or oversized pages',()=>{
  for(const page of [null,[],{items:[alert(),alert()],next_cursor:null},
   {items:[],next_cursor:42},{items:[],next_cursor:''},
   {items:[],next_cursor:'x'.repeat(8193)},
   {items:Array.from({length:101},alert),next_cursor:null}]){
   expect(()=>readCaptureAlertPage(page,id)).toThrow();
  }
 });
 it('accepts complete acknowledgement and signed cursor',()=>{
  const row={...alert(),acknowledged_by:id,acknowledged_at:'2026-10-11T12:01:00Z'};
  expect(readCaptureAlertPage({items:[row],next_cursor:'signed-cursor'},id)).toEqual({items:[row],next_cursor:'signed-cursor'});
 });
});
