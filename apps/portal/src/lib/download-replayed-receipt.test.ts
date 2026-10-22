import {expect,it} from 'vitest';
import {downloadNeedsPolling} from './download-state';
it('fetches complete inventory for a terminal idempotent Start receipt and stops after reconciliation',()=>{
 for(const state of ['ready','failed','expired','cancelled']){
  expect(downloadNeedsPolling(state,true,true)).toBe(true);
  expect(downloadNeedsPolling(state,false,true)).toBe(false);
 }
});
it('pauses both normal and receipt recovery while hidden, resuming only in foreground',()=>{
 expect(downloadNeedsPolling('building',false,false)).toBe(false);
 expect(downloadNeedsPolling('ready',true,false)).toBe(false);
 expect(downloadNeedsPolling('ready',true,true)).toBe(true);
});
