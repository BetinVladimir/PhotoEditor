// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {ExportPreset} from './catalog-types';
const local=vi.hoisted(()=>({data:new Map<string,string>()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({Link:({children}:any)=><a>{children}</a>}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:unknown,action:()=>Promise<unknown>)=>action()}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(key:string)=>local.data.get(key)??null,setItem:async(key:string,value:string)=>{local.data.set(key,value)},removeItem:async(key:string)=>{local.data.delete(key)}}}));
import {ExportBatchPanel} from './export-batch-panel';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const origin='https://example.test',tenant=id(2),snapshot=id(3),actor=id(1),preset=id(4);
const key='photo.export-batch-attempt.v1:'+JSON.stringify([origin,actor,tenant,snapshot]);
const ticket={batch_id:id(5),project_id:id(6),state:'queued',groups:[{key:id(7),package_id:id(8),metadata_policy:'strip',presets:[preset],ticket:{run_id:id(9),workflow_id:id(10),state:'queued'},failure_code:null}]};
function fixture(accepted:boolean){
 const attempt={key:id(11),request:{snapshot_id:snapshot,presets:[preset],approved_fields:[]},ticket:accepted?ticket:null};local.data.set(key,JSON.stringify(attempt));
 const api={me:vi.fn().mockResolvedValue({id:actor}),exportBatchStatus:vi.fn().mockResolvedValue({...ticket,state:'running',groups:[{...ticket.groups[0],run_status:'running'}]}),createExportBatch:vi.fn()};
 const ui=()=> <ExportBatchPanel api={api as unknown as OwnerAPI} origin={origin} tenant={tenant} snapshot={snapshot} presets={[{id:preset,name:'Photo',config:{metadata_policy:'strip'}} as ExportPreset]}/>;
 return {api,attempt,ui};
}
beforeEach(()=>local.data.clear());
afterEach(()=>{cleanup();vi.useRealTimers()});
it.each([401,403,404,410])('withdraws package links on polling denial %s and retains the journal',async(status)=>{
 const f=fixture(true);f.api.exportBatchStatus.mockRejectedValue(new APIError(status,'DENIED'));render(f.ui());await screen.findByText(/Export access is no longer available/);
 expect(screen.queryByText('Open package')).toBeNull();expect(JSON.parse(local.data.get(key)!)).toEqual(f.attempt);
 vi.useFakeTimers();await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(f.api.exportBatchStatus).toHaveBeenCalledTimes(1);expect(f.api.createExportBatch).not.toHaveBeenCalled();
});
it.each([401,403,404,410])('withdraws admission authority on denial %s without deleting the request',async(status)=>{
 const f=fixture(false);f.api.createExportBatch.mockRejectedValue(new APIError(status,'DENIED'));render(f.ui());await screen.findByText('Recover export batch');
 await waitFor(()=>expectEnabled(screen.getByText('Recover export batch')));
 await clickWhenEnabled(screen.getByText('Recover export batch'));await screen.findByText(/Export access is no longer available/);
 expect(f.api.createExportBatch).toHaveBeenCalledTimes(1);expect(f.api.exportBatchStatus).not.toHaveBeenCalled();expect(JSON.parse(local.data.get(key)!)).toEqual(f.attempt);
});
it('retries transient polling without creating another package or deleting its journal',async()=>{
 const f=fixture(true);let reject!:(error:Error)=>void;f.api.exportBatchStatus.mockImplementationOnce(()=>new Promise((_resolve,no)=>{reject=no}));render(f.ui());await screen.findByText('Open package');await waitFor(()=>expect(f.api.exportBatchStatus).toHaveBeenCalledTimes(1));vi.useFakeTimers();
 await act(async()=>reject(new Error('network')));expect(screen.getByText('Unable to refresh package progress')).toBeTruthy();expect(screen.getByText('Open package')).toBeTruthy();
 await act(async()=>{await vi.advanceTimersByTimeAsync(10000)});expect(f.api.exportBatchStatus).toHaveBeenCalledTimes(2);expect(screen.queryByText('Unable to refresh package progress')).toBeNull();expect(f.api.createExportBatch).not.toHaveBeenCalled();expect(JSON.parse(local.data.get(key)!)).toEqual(f.attempt);
});
it('ignores late polling completion after unmount and retains its accepted receipt',async()=>{
 const f=fixture(true);let complete!:(value:unknown)=>void;f.api.exportBatchStatus.mockImplementation(()=>new Promise(resolve=>{complete=resolve}));const view=render(f.ui());await screen.findByText('Open package');await waitFor(()=>expect(f.api.exportBatchStatus).toHaveBeenCalledTimes(1));view.unmount();
 await act(async()=>complete({...ticket,state:'completed',groups:[{...ticket.groups[0],run_status:'completed'}]}));expect(JSON.parse(local.data.get(key)!)).toEqual(f.attempt);expect(f.api.createExportBatch).not.toHaveBeenCalled();
});

it('accepts terminal child errors without a polling loop or replacing accepted packages',async()=>{
 const f=fixture(true);
 f.api.exportBatchStatus.mockResolvedValue({...ticket,state:'failed',groups:[{...ticket.groups[0],run_status:'completed_with_errors'}]});
 render(f.ui());await screen.findByText(/completed_with_errors/);
 expect(screen.queryByText('Unable to refresh package progress')).toBeNull();
 expect(screen.getByText('Open package')).toBeTruthy();
 vi.useFakeTimers();await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});
 expect(f.api.exportBatchStatus).toHaveBeenCalledTimes(1);
 expect(f.api.createExportBatch).not.toHaveBeenCalled();
 expect(JSON.parse(local.data.get(key)!)).toEqual(f.attempt);
});

it('recovers a newly authorized scope after the initial actor lookup was denied',async()=>{
 const f=fixture(true);f.api.me.mockRejectedValue(new APIError(403,'DENIED'));
 const view=render(f.ui());await screen.findByText('Export access is unavailable');
 const restored=fixture(true);
 view.rerender(restored.ui());await screen.findByText('Open package');
 expect(screen.queryByText('Export access is unavailable')).toBeNull();
 expect(restored.api.createExportBatch).not.toHaveBeenCalled();
 expect(JSON.parse(local.data.get(key)!)).toEqual(restored.attempt);
});
it('cancels uncertain admission without losing its frozen journal and rejects a late ticket',async()=>{
 const f=fixture(false);let finish!:(value:unknown)=>void;
 f.api.createExportBatch.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce(ticket);
 render(f.ui());const recover=await screen.findByRole('button',{name:'Recover export batch'});await waitFor(()=>{expect(recover.hasAttribute('disabled')).toBe(false);expectEnabled(recover)});
 fireEvent.click(recover);await waitFor(()=>expect(f.api.createExportBatch).toHaveBeenCalledTimes(1));
 fireEvent.click(screen.getByText('Stop loading'));await act(async()=>finish(ticket));expect(screen.queryByText('Open package')).toBeNull();
 const retry=screen.getByRole('button',{name:'Recover export batch'});await waitFor(()=>{expect(retry.hasAttribute('disabled')).toBe(false);expectEnabled(retry)});fireEvent.click(retry);await screen.findByText('Open package');
 expect(f.api.createExportBatch.mock.calls[1][0]).toEqual(f.attempt.request);expect(f.api.createExportBatch.mock.calls[1][1]).toBe(f.attempt.key);
});
it('retries a failed initial recovery through readonly identity and journal reads',async()=>{
 const f=fixture(true);f.api.me.mockRejectedValueOnce(new Error('offline'));
 render(f.ui());fireEvent.click(await screen.findByText('Retry loading'));await screen.findByText('Open package');
 expect(f.api.me).toHaveBeenCalledTimes(2);expect(f.api.createExportBatch).not.toHaveBeenCalled();
});
it('bounds a hung initial recovery and ignores its late actor before readonly Retry',async()=>{
 const f=fixture(true);let finish!:(value:unknown)=>void;f.api.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 vi.useFakeTimers();render(f.ui());await act(async()=>{await vi.advanceTimersByTimeAsync(8000)});
 expect(screen.getByText('Retry loading')).toBeTruthy();await act(async()=>finish({id:actor}));expect(screen.queryByText('Open package')).toBeNull();
 vi.useRealTimers();fireEvent.click(screen.getByText('Retry loading'));await screen.findByText('Open package');expect(f.api.createExportBatch).not.toHaveBeenCalled();
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
