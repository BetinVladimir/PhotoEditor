// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {APIError,type UploadSource} from '@photoeditor/api-client';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import AsyncStorage from '@react-native-async-storage/async-storage';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
vi.mock('@react-native-async-storage/async-storage',()=>{const values=new Map<string,string>();return {default:{getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}}};});
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async<T>(_key:string,action:()=>Promise<T>)=>action()}));
vi.mock('./interchange-archive-controller',async importOriginal=>({...await importOriginal<typeof import('./interchange-archive-controller')>(),runArchiveUpload:vi.fn(),abortArchiveUpload:vi.fn(),resetAbortedArchiveUpload:vi.fn()}));
import {runArchiveUpload,resetAbortedArchiveUpload} from './interchange-archive-controller';
import {ownArchiveSource} from './interchange-archive-source-lifecycle';
import {OwnerAPI} from './owner-api';
import {archiveArtifactID,archiveUploadID,archiveRequestSHA,type ArchiveStatus} from './interchange-archive-upload';
import {InterchangeArchiveAttemptStore} from './interchange-archive-attempt-store';
import {InterchangeArchiveScreen} from './interchange-archive-screen';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const source:UploadSource={name:'archive.zip',size:22,sha256:'a'.repeat(64),lastModified:0,read:async()=>new Uint8Array(22)};
function props(){return {client:{request:vi.fn()} as never,origin:'https://archive-screen.test',user:id(9),tenant:id(1),project:id(2),picker:vi.fn(async()=>source),onReady:vi.fn()};}
afterEach(()=>{cleanup();vi.mocked(runArchiveUpload).mockReset();vi.mocked(resetAbortedArchiveUpload).mockReset();vi.restoreAllMocks();});
it('shows saved no-receipt Begin reconciliation after explicit read without enabling receipt actions',async()=>{
 const p=props();p.origin=`https://archive-screen.test/${crypto.randomUUID()}`;
 const request={filename:source.name,bytes:source.size,sha256:source.sha256};
 const journal=new InterchangeArchiveAttemptStore(AsyncStorage,p.origin,p.user,p.tenant,p.project,request);
 await journal.write({version:1,key:'saved-key',body:request,checkpoint:null});
 const status:ArchiveStatus={upload_id:archiveUploadID(p.tenant,p.user,'saved-key'),state:'reserved',request_sha256:archiveRequestSHA(p.project,request),expires_at:new Date(Date.now()+3600000).toISOString(),ready:false,begin_recovery:'reconciliation_required'};
 const read=vi.spyOn(OwnerAPI.prototype,'interchangeArchiveAttemptStatus').mockResolvedValue(status);
 render(<InterchangeArchiveScreen {...p}/>);
 fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');
 expect(screen.getByText('interchange_archive_check')).toBeTruthy();expect(read).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('interchange_archive_check'));await screen.findByText('interchange_archive_reconciliation');
 expect(read).toHaveBeenCalledOnce();expect(runArchiveUpload).not.toHaveBeenCalled();
 expect(screen.queryByText('interchange_archive_abort')).toBeNull();expect(screen.queryByText('interchange_archive_open')).toBeNull();
});
it('does not upload on mount or file selection; explicit action starts it',async()=>{
 const p=props();vi.mocked(runArchiveUpload).mockRejectedValue(new Error('failed'));
 render(<InterchangeArchiveScreen {...p}/>);expect(p.picker).not.toHaveBeenCalled();expect(runArchiveUpload).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');expect(runArchiveUpload).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('interchange_archive_upload'));await screen.findByRole('alert');expect(runArchiveUpload).toHaveBeenCalledOnce();expect(p.onReady).not.toHaveBeenCalled();
});
it('keeps the selected file and shows the open-Begin reconciliation state',async()=>{
 const p=props();vi.mocked(runArchiveUpload).mockRejectedValue(new APIError(409,'ARCHIVE_BEGIN_RECONCILIATION_REQUIRED'));
 render(<InterchangeArchiveScreen {...p}/>);
 fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');
 fireEvent.click(screen.getByText('interchange_archive_upload'));
 expect((await screen.findByRole('alert')).textContent).toBe('interchange_archive_reconciliation');
 expect(screen.getByText('archive.zip')).toBeTruthy();expect(p.onReady).not.toHaveBeenCalled();
});
it('aborts active upload on unmount',async()=>{
 const p=props();let signal:AbortSignal|undefined;
 vi.mocked(runArchiveUpload).mockImplementation(async options=>{signal=options.signal;return new Promise((_resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new Error('aborted')),{once:true}));});
 const view=render(<InterchangeArchiveScreen {...p}/>);fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');fireEvent.click(screen.getByText('interchange_archive_upload'));
 await waitFor(()=>expect(signal).toBeDefined());view.unmount();expect(signal!.aborted).toBe(true);expect(p.onReady).not.toHaveBeenCalled();
});

it('drops the reset attempt even if cache deletion fails and retries deletion on next selection',async()=>{
 const p=props(),remove=vi.fn().mockImplementationOnce(()=>{throw new Error('cache busy');});
 const owned=ownArchiveSource(source,remove);
 p.picker.mockResolvedValueOnce(owned).mockResolvedValueOnce(source);
 vi.mocked(runArchiveUpload).mockResolvedValue({upload_id:id(3),state:'aborted',request_sha256:'b'.repeat(64),expires_at:'2030-01-01T00:00:00Z',ready:false});
 vi.mocked(resetAbortedArchiveUpload).mockResolvedValue(undefined);
 render(<InterchangeArchiveScreen {...p}/>);
 fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');
 fireEvent.click(screen.getByText('interchange_archive_upload'));await screen.findByText('interchange_archive_restart');
 fireEvent.click(screen.getByText('interchange_archive_restart'));await screen.findByRole('alert');
 expect(resetAbortedArchiveUpload).toHaveBeenCalledOnce();expect(remove).toHaveBeenCalledTimes(1);
 expect(screen.queryByText('archive.zip')).toBeNull();expect(screen.queryByText('interchange_archive_restart')).toBeNull();
 expect(screen.queryByText('interchange_archive_upload')).toBeNull();
 await expect(owned.read(0,22,new AbortController().signal)).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_RELEASED');
 fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');
 expect(remove).toHaveBeenCalledTimes(2);expect(screen.queryByRole('alert')).toBeNull();
 expect(runArchiveUpload).toHaveBeenCalledOnce();
});
it('retains the selected attempt when server-confirmed reset fails',async()=>{
 const p=props();
 vi.mocked(runArchiveUpload).mockResolvedValue({upload_id:id(3),state:'aborted',request_sha256:'b'.repeat(64),expires_at:'2030-01-01T00:00:00Z',ready:false});
 vi.mocked(resetAbortedArchiveUpload).mockRejectedValue(new Error('status unavailable'));
 render(<InterchangeArchiveScreen {...p}/>);
 fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');
 fireEvent.click(screen.getByText('interchange_archive_upload'));await screen.findByText('interchange_archive_restart');
 fireEvent.click(screen.getByText('interchange_archive_restart'));await screen.findByRole('alert');
 expect(screen.getByText('archive.zip')).toBeTruthy();expect(screen.getByText('interchange_archive_restart')).toBeTruthy();
});

async function mountReadyArchive(){
 const p=props();p.origin=`https://archive-screen.test/${crypto.randomUUID()}`;
 const request={filename:source.name,bytes:source.size,sha256:source.sha256};
 const ready:ArchiveStatus={upload_id:id(3),state:'verified',request_sha256:archiveRequestSHA(p.project,request),expires_at:'2030-01-01T00:00:00Z',ready:true,artifact:{artifact_id:archiveArtifactID(id(3)),sha256:source.sha256}};
 let removeJournal!:()=>Promise<void>;
 vi.mocked(runArchiveUpload).mockImplementation(async options=>{
  await options.journal.write({version:1,key:'ready-attempt',body:request,checkpoint:{stage:'queued',parts:[{part_no:1,etag:'part-one'}],receipt:{upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:ready.request_sha256}}});
  removeJournal=()=>options.journal.clear('ready-attempt');return ready;
 });
 const view=render(<InterchangeArchiveScreen {...p}/>);
 fireEvent.click(screen.getByText('interchange_archive_pick'));await screen.findByText('archive.zip');
 fireEvent.click(screen.getByText('interchange_archive_upload'));await screen.findByText('interchange_archive_open');
 return {p,ready,view,removeJournal:()=>removeJournal()};
}
it('uses a fresh ready response and opens the import only once',async()=>{
 const {p,ready}=await mountReadyArchive();
 const status=vi.spyOn(OwnerAPI.prototype,'interchangeArchiveStatus').mockResolvedValue(ready);
 fireEvent.click(screen.getByText('interchange_archive_open'));fireEvent.click(screen.getByText('interchange_archive_open'));
 await waitFor(()=>expect(p.onReady).toHaveBeenCalledOnce());expect(p.onReady).toHaveBeenCalledWith(ready.artifact);expect(status).toHaveBeenCalledOnce();
 fireEvent.click(screen.getByText('interchange_archive_open'));expect(p.onReady).toHaveBeenCalledOnce();
});
it.each(['completing','aborting','aborted','verified'])('replaces stale ready UI with fresh non-ready state: %s',async state=>{
 const {p,ready}=await mountReadyArchive();
 vi.spyOn(OwnerAPI.prototype,'interchangeArchiveStatus').mockResolvedValue({...ready,state,ready:false,artifact:undefined});
 fireEvent.click(screen.getByText('interchange_archive_open'));await screen.findByRole('alert');expect(p.onReady).not.toHaveBeenCalled();
 expect(screen.queryByText('interchange_archive_open')).toBeNull();expect(screen.queryByText('interchange_archive_ready')).toBeNull();
 expect(screen.getByText(state==='aborted'?'interchange_archive_aborted':state==='aborting'?'interchange_archive_aborting':'interchange_archive_pending')).toBeTruthy();
});
it('refuses a ready response if its durable journal disappeared during the request',async()=>{
 const {p,ready,removeJournal}=await mountReadyArchive();
 vi.spyOn(OwnerAPI.prototype,'interchangeArchiveStatus').mockImplementation(async()=>{await removeJournal();return ready;});
 fireEvent.click(screen.getByText('interchange_archive_open'));await screen.findByRole('alert');expect(p.onReady).not.toHaveBeenCalled();
});
it('cancels a pending status request and suppresses handoff after unmount',async()=>{
 const {p,ready,view}=await mountReadyArchive();let signal:AbortSignal|undefined,resolve!:(status:ArchiveStatus)=>void;
 vi.spyOn(OwnerAPI.prototype,'interchangeArchiveStatus').mockImplementation(async(_project,_request,_receipt,currentSignal)=>{
  signal=currentSignal;return new Promise(done=>{resolve=done;});
 });
 fireEvent.click(screen.getByText('interchange_archive_open'));await waitFor(()=>expect(signal).toBeDefined());
 view.unmount();expect(signal!.aborted).toBe(true);await act(async()=>{resolve(ready);});
 await waitFor(()=>expect(screen.queryByText('interchange_archive_open')).toBeNull());expect(p.onReady).not.toHaveBeenCalled();
});
