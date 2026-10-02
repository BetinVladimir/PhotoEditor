import {describe,it,expect} from 'vitest';
import {mergeCaptureAlerts} from './capture-alert-merge';
import type {CaptureAlert} from './capture-types';
const alert=(id:string,revision:string,created_at:string,acknowledged_at:string|null=null):CaptureAlert=>({id,revision,created_at,acknowledged_at,acknowledged_by:null,session_id:'session',asset_version_id:'version',report_artifact_id:'report',receipt_id:'receipt',severity:'info',qa_stale:false});
describe('Capture alert refresh',()=>{
 it('retains loaded history while adding new reports in descending order',()=>{
  expect(mergeCaptureAlerts([alert('old','1','2026-01-01')],[alert('new','1','2026-01-02')]).map(x=>x.id)).toEqual(['new','old']);
 });
 it('cannot overwrite a completed acknowledgement with an older read',()=>{
  const acknowledged=alert('one','2','2026-01-01','2026-01-02');
  expect(mergeCaptureAlerts([acknowledged],[alert('one','1','2026-01-01')])).toEqual([acknowledged]);
 });
 it('compares revision strings without losing integer precision',()=>{
  const updated=alert('one','9007199254740993','2026-01-01','2026-01-02');
  expect(mergeCaptureAlerts([alert('one','9007199254740992','2026-01-01')],[updated])).toEqual([updated]);
 });
 it('deduplicates equal revisions and orders same-time reports by ID',()=>{
  expect(mergeCaptureAlerts([alert('a','9','2026-01-01')],[alert('a','9','2026-01-01'),alert('b','10','2026-01-01')]).map(x=>x.id)).toEqual(['b','a']);
 });
 it('restores immutable confirmation at an unchanged alert revision and retains it through stale reads',()=>{
  const original=alert('one','1','2026-01-01');
  const confirmed={...original,confirmation_id:'confirmation'};
  const merged=mergeCaptureAlerts([original],[confirmed]);
  expect(merged[0].confirmation_id).toBe('confirmation');
  expect(mergeCaptureAlerts(merged,[{...original,revision:'2'}])[0].confirmation_id).toBe('confirmation');
 });

});
