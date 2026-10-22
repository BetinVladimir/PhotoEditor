// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native,Image:({source,accessibilityLabel,onLoad,onError}:any)=>React.createElement('img',{src:source?.uri,'aria-label':accessibilityLabel,onLoad,onError})}});
const journal=vi.hoisted(()=>({read:vi.fn(async()=>null as unknown),write:vi.fn(async(_record:unknown)=>{}),clear:vi.fn(async()=>{}),superseded:vi.fn(async(_key:string,_statuses:unknown)=>{})}));
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
const mocks=vi.hoisted(()=>({entries:vi.fn(),candidates:vi.fn(),preview:vi.fn(),media:vi.fn(),decide:vi.fn(),statuses:vi.fn(),me:vi.fn(async()=>({id:'00000001-0000-4000-8000-000000000001'}))}));
vi.mock('./owner-api',()=>({OwnerAPI:class {
  me=mocks.me;importPersonMaskEntries=mocks.entries;importPersonMaskCandidates=mocks.candidates;importPersonMaskPreview=mocks.preview;mediaLinks=mocks.media;decideImportPersonMask=mocks.decide;importPersonMaskStatuses=mocks.statuses;
}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./import-mask-attempt-store',()=>({ImportMaskAttemptStore:class {read=journal.read;write=journal.write;clear=journal.clear;clearSuperseded=journal.superseded;}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {APIError} from '@photoeditor/api-client';
import {InterchangePersonMaskScreen} from './interchange-person-mask-screen';
const id=(n:number)=>`${n.toString(16).padStart(8,'0')}-0000-4000-8000-000000000001`;
const png=()=>{const bytes=new Uint8Array(33);bytes.set([137,80,78,71,13,10,26,10,0,0,0,13,73,72,68,82,0,0,4,0,0,0,4,0]);return bytes;};
afterEach(()=>{cleanup();lifecycle.active=true;lifecycle.generation=0;vi.restoreAllMocks();mocks.statuses.mockReset();journal.read.mockReset().mockResolvedValue(null);journal.write.mockReset().mockResolvedValue(undefined);journal.clear.mockReset().mockResolvedValue(undefined);journal.superseded.mockReset().mockResolvedValue(undefined);});
it('requires both photo and verified mask to render before enabling grant',async()=>{
  mocks.statuses.mockResolvedValue([]);
  const entry={entry_id:id(1),target_version_id:id(2),asset_id:id(3),external_id:'frame-1'};
  const decision={action:'grant' as const,entry_id:entry.entry_id,target_version_id:entry.target_version_id,override_artifact_id:id(4),mask_artifact_id:id(5),parsed_jcs:'{}',recipe_artifact_id:id(6),recipe_jcs:'{}',override_jcs:'{}',mask_jcs:JSON.stringify({canvas:{width:1024,height:1024}})};
  mocks.entries.mockResolvedValue({entries:[entry]});mocks.candidates.mockResolvedValue({candidates:[{external_id:entry.external_id,decision}]});mocks.preview.mockResolvedValue(png());
  mocks.media.mockResolvedValue({items:[{asset_version_id:entry.target_version_id,variant:'preview1600',media_type:'image/jpeg',url:'https://example.test/photo.jpg',expires_at:new Date(Date.now()+60000).toISOString(),width:1024,height:1024}]});
  Object.defineProperty(URL,'createObjectURL',{configurable:true,value:vi.fn(()=> 'blob:mask')});Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:vi.fn()});
  Object.defineProperty(globalThis,'createImageBitmap',{configurable:true,value:vi.fn(async()=>({width:1024,height:1024,close:()=>{}}))});
  const client={origin:'https://example.test'};
  const view=render(<InterchangePersonMaskScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
  await clickWhenEnabled(await screen.findByRole('button',{name:'frame-1'}));
  await clickWhenEnabled(await screen.findByRole('button',{name:/interchange_mask_candidate/}));
  const grant=await screen.findByRole('button',{name:'interchange_mask_grant'});
  fireEvent.click(grant);expect(mocks.decide).not.toHaveBeenCalled();
  fireEvent.load(screen.getByLabelText('interchange_mask_photo'));fireEvent.load(screen.getByLabelText('interchange_mask_overlay'));
  await waitFor(()=>expect((grant as HTMLButtonElement).disabled).toBe(false));
  const firstPhoto=screen.getByLabelText('interchange_mask_photo');
  await clickWhenEnabled(screen.getByRole('button',{name:'interchange_mask_refresh_preview'}));
  await waitFor(()=>expect(screen.getByLabelText('interchange_mask_photo')).not.toBe(firstPhoto));
  fireEvent.load(firstPhoto);expect((screen.getByRole('button',{name:'interchange_mask_grant'}) as HTMLButtonElement).disabled).toBe(true);
  expect(mocks.decide).not.toHaveBeenCalled();
  fireEvent.load(screen.getByLabelText('interchange_mask_photo'));fireEvent.load(screen.getByLabelText('interchange_mask_overlay'));
  await waitFor(()=>expect((screen.getByRole('button',{name:'interchange_mask_grant'}) as HTMLButtonElement).disabled).toBe(false));
  const oldPhoto=screen.getByLabelText('interchange_mask_photo');
  lifecycle.active=false;view.rerender(<InterchangePersonMaskScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
  expect(screen.queryByLabelText('interchange_mask_photo')).toBeNull();expect(screen.queryByRole('button',{name:'interchange_mask_grant'})).toBeNull();
  lifecycle.active=true;lifecycle.generation++;view.rerender(<InterchangePersonMaskScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
  await clickWhenEnabled(await screen.findByRole('button',{name:'frame-1'}));
  await clickWhenEnabled(await screen.findByRole('button',{name:/interchange_mask_candidate/}));
  const freshGrant=await screen.findByRole('button',{name:'interchange_mask_grant'});
  fireEvent.error(oldPhoto);fireEvent.load(oldPhoto);
  expect(screen.getByLabelText('interchange_mask_photo')).not.toBe(oldPhoto);
  expect((freshGrant as HTMLButtonElement).disabled).toBe(true);
  fireEvent.load(screen.getByLabelText('interchange_mask_photo'));fireEvent.load(screen.getByLabelText('interchange_mask_overlay'));
  await waitFor(()=>expect((freshGrant as HTMLButtonElement).disabled).toBe(false));
  const originalNow=Date.now();const clock=vi.spyOn(Date,'now').mockReturnValue(originalNow+120000);
  fireEvent.click(freshGrant);expect(mocks.decide).not.toHaveBeenCalled();clock.mockRestore();
  mocks.decide.mockResolvedValue({status:'granted',epoch:1});fireEvent.click(freshGrant);
  await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),{...decision,expected_epoch:0},expect.any(AbortSignal)));
  await waitFor(()=>expect((freshGrant as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(freshGrant);await Promise.resolve();expect(mocks.decide).toHaveBeenCalledTimes(1);
});
it('lets the editor revoke a saved grant after candidate listing is unavailable',async()=>{
  const saved={entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5),status:'granted',epoch:3};
  mocks.statuses.mockResolvedValue([saved]);mocks.entries.mockRejectedValue(new Error('not running'));
  mocks.decide.mockResolvedValue({status:'revoked',epoch:4});
  render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
  await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_revoke'}));
  await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),{action:'revoke',expected_epoch:3,entry_id:saved.entry_id,target_version_id:saved.target_version_id,override_artifact_id:saved.override_artifact_id,mask_artifact_id:saved.mask_artifact_id},expect.any(AbortSignal)));
  await screen.findByText(/interchange_mask_revoked/);
});

it('replays an uncertain saved revoke without any preview',async()=>{
 const item={entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5),status:'granted',epoch:1};
 mocks.entries.mockResolvedValue({entries:[]});mocks.statuses.mockResolvedValue([item]);
 mocks.decide.mockReset();mocks.decide.mockRejectedValueOnce(new Error('lost')).mockResolvedValueOnce({workflow_id:id(9),entry_id:id(1),status:'revoked',epoch:2});
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_revoke'}));
 const retry=await screen.findByRole('button',{name:'interchange_mask_retry'});await waitFor(()=>expect((retry as HTMLButtonElement).disabled).toBe(false));
 const original=structuredClone(mocks.decide.mock.calls[0][2]);fireEvent.click(retry);
 await waitFor(()=>expect(screen.queryByRole('button',{name:'interchange_mask_retry'})).toBeNull());
 expect(mocks.decide.mock.calls[1][2]).toEqual(original);expect(original.expected_epoch).toBe(1);
});

it('restores a durable decision when catalogue reads fail',async()=>{
 const body={action:'revoke',expected_epoch:3,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5)};
 journal.read.mockResolvedValue({version:1,key:id(12),body});mocks.entries.mockRejectedValue(new Error('stopped'));mocks.statuses.mockRejectedValue(new Error('offline'));
 mocks.decide.mockReset().mockResolvedValue({workflow_id:id(9),entry_id:id(1),status:'revoked',epoch:4});
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_retry'}));
 await waitFor(()=>expect(mocks.decide).toHaveBeenCalledWith(id(8),id(9),body,expect.any(AbortSignal)));
 expect(journal.write).toHaveBeenCalledWith({version:1,key:id(12),body});await waitFor(()=>expect(journal.clear).toHaveBeenCalled());
});
it('retains a saved attempt but never submits after background during journal write',async()=>{
 const body={action:'revoke',expected_epoch:3,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5)};
 journal.read.mockResolvedValue({version:1,key:id(12),body});mocks.entries.mockResolvedValue({entries:[]});mocks.statuses.mockResolvedValue([]);mocks.decide.mockReset();
 let finish!:()=>void;journal.write.mockImplementationOnce(()=>new Promise<void>(done=>{finish=done}));
 const client={origin:'https://example.test'},view=render(<InterchangePersonMaskScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_retry'}));await waitFor(()=>expect(journal.write).toHaveBeenCalled());
 lifecycle.active=false;view.rerender(<InterchangePersonMaskScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);finish();
 await screen.findByText('read_paused');expect(screen.queryByRole('button',{name:'interchange_mask_retry'})).toBeNull();
 expect(mocks.decide).not.toHaveBeenCalled();expect(journal.clear).not.toHaveBeenCalled();
});

it('reconciles an exact CAS conflict through authenticated newer status without claiming success',async()=>{
 const body={action:'revoke',expected_epoch:1,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5)};
 const status={entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5),status:'granted',epoch:3};
 journal.read.mockResolvedValue({version:1,key:id(12),body});mocks.entries.mockResolvedValue({entries:[]});mocks.statuses.mockResolvedValue([status]);
 mocks.decide.mockReset().mockRejectedValue(new APIError(409,'INTERCHANGE_MASK_REVISION_CONFLICT'));
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_retry'}));
 await waitFor(()=>expect(journal.superseded).toHaveBeenCalledWith(id(12),[status]));
 await waitFor(()=>expect(screen.queryByRole('button',{name:'interchange_mask_retry'})).toBeNull());
 expect(journal.clear).not.toHaveBeenCalled();expect(screen.queryByText('interchange_mask_revoked')).toBeNull();
});

it('reloads a failed journal read without discarding its saved decision',async()=>{
 const body={action:'revoke',expected_epoch:1,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5)};
 journal.read.mockRejectedValueOnce(new Error('storage offline')).mockResolvedValueOnce({version:1,key:id(12),body});mocks.entries.mockResolvedValue({entries:[]});mocks.statuses.mockResolvedValue([]);mocks.decide.mockReset();
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_reload'}));await screen.findByRole('button',{name:'interchange_mask_retry'});
 expect(journal.read).toHaveBeenCalledTimes(2);expect(journal.write).not.toHaveBeenCalled();expect(journal.clear).not.toHaveBeenCalled();expect(mocks.decide).not.toHaveBeenCalled();
});

it('never sends a request when durable write fails and retains the exact retry',async()=>{
 const body={action:'revoke',expected_epoch:1,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5)};
 journal.read.mockResolvedValue({version:1,key:id(12),body});journal.write.mockRejectedValueOnce(new Error('disk full'));
 mocks.entries.mockResolvedValue({entries:[]});mocks.statuses.mockResolvedValue([]);mocks.decide.mockReset().mockResolvedValue({workflow_id:id(9),entry_id:id(1),status:'revoked',epoch:2});
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_retry'}));
 await screen.findByRole('alert');expect(mocks.decide).not.toHaveBeenCalled();expect(journal.clear).not.toHaveBeenCalled();
 await clickWhenEnabled(screen.getByRole('button',{name:'interchange_mask_retry'}));await waitFor(()=>expect(mocks.decide).toHaveBeenCalledOnce());
 expect(journal.write.mock.calls[0][0]).toEqual(journal.write.mock.calls[1][0]);
});
it('does not resurrect an acknowledged attempt when background arrives during journal clear',async()=>{
 const body={action:'revoke',expected_epoch:1,entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5)};
 journal.read.mockResolvedValue({version:1,key:id(12),body});mocks.entries.mockResolvedValue({entries:[]});mocks.statuses.mockResolvedValue([]);mocks.decide.mockReset().mockResolvedValue({workflow_id:id(9),entry_id:id(1),status:'revoked',epoch:2});
 let finish!:()=>void;journal.clear.mockImplementationOnce(()=>new Promise<void>(done=>{finish=done}));
 const client={origin:'https://example.test'},view=render(<InterchangePersonMaskScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_retry'}));await waitFor(()=>expect(journal.clear).toHaveBeenCalled());
 lifecycle.active=false;view.rerender(<InterchangePersonMaskScreen client={client as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);finish();
 await waitFor(()=>expect(screen.queryByRole('button',{name:'interchange_mask_retry'})).toBeNull());expect(mocks.decide).toHaveBeenCalledOnce();
});
it('retries failed entry catalogue without recreating or submitting a decision',async()=>{
 mocks.entries.mockReset();mocks.decide.mockClear();
 mocks.entries.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({entries:[]});mocks.statuses.mockResolvedValue([]);mocks.me.mockResolvedValue({id:id(1)});
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_reload'}));
 await waitFor(()=>expect(mocks.entries).toHaveBeenCalledTimes(2));expect(mocks.decide).not.toHaveBeenCalled();expect(journal.write).not.toHaveBeenCalled();
});

it('cancels a saved revoke and preserves exact retry after refusing a late old receipt',async()=>{
 const item={entry_id:id(1),target_version_id:id(2),override_artifact_id:id(4),mask_artifact_id:id(5),status:'granted',epoch:3};
 mocks.entries.mockResolvedValue({entries:[]});mocks.statuses.mockResolvedValue([item]);let finish!:(value:unknown)=>void;
 mocks.decide.mockReset().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({status:'revoked',epoch:4});
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);
 await clickWhenEnabled(await screen.findByRole('button',{name:'interchange_mask_revoke'}));await waitFor(()=>expect(mocks.decide).toHaveBeenCalledOnce());const body=structuredClone(mocks.decide.mock.calls[0][2]);
 await clickWhenEnabled(screen.getByRole('button',{name:'read_cancel'}));await act(async()=>finish({status:'revoked',epoch:4}));expect(journal.clear).not.toHaveBeenCalled();
 await clickWhenEnabled(screen.getByRole('button',{name:'interchange_mask_retry'}));await waitFor(()=>expect(mocks.decide).toHaveBeenCalledTimes(2));expect(mocks.decide.mock.calls[1][2]).toEqual(body);
});
it('cancels initial entries and retries readonly without creating a mask decision',async()=>{
 mocks.decide.mockReset();mocks.entries.mockReset();
 let finish!:(value:unknown)=>void;mocks.entries.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({entries:[]});mocks.statuses.mockResolvedValue([]);
 render(<InterchangePersonMaskScreen client={{origin:'https://example.test'} as never} tenant={id(7)} project={id(8)} workflow={id(9)}/>);await waitFor(()=>expect(mocks.entries).toHaveBeenCalled());
 await clickWhenEnabled(screen.getByRole('button',{name:'read_cancel'}));await act(async()=>finish({entries:[{entry_id:id(1),target_version_id:id(2),asset_id:id(3),external_id:'late-frame'}]}));expect(screen.queryByRole('button',{name:'late-frame'})).toBeNull();
 await clickWhenEnabled(screen.getByRole('button',{name:'interchange_mask_reload'}));await screen.findByText('interchange_mask_entries_empty');expect(mocks.decide).not.toHaveBeenCalled();
});
