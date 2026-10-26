// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';

const mocks=vi.hoisted(()=>{
 const first={request:vi.fn()},second={request:vi.fn()};const storage=new Map<string,string>();
 return {storage,first,second,auth:{origin:'https://example.test',client:()=>first},params:{tenant:'00000000-0000-4000-8000-000000000010',snapshot:'00000000-0000-4000-8000-000000000011'},presets:vi.fn(),createPreset:vi.fn(),startDownload:vi.fn()};
});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:unknown,action:()=>Promise<unknown>)=>action()}));
// This standalone screen fixture explicitly grants current workspace edit authority.
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'00000000-0000-4000-8000-000000000001',canEdit:true,canAdmin:true})}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {me=async()=>({id:'00000000-0000-4000-8000-000000000001'});presets=mocks.presets;createPreset=mocks.createPreset;startDownload=mocks.startDownload;}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:vi.fn(async(key:string)=>mocks.storage.get(key)??null),setItem:vi.fn(async(key:string,value:string)=>{mocks.storage.set(key,value)}),removeItem:vi.fn(async(key:string)=>{mocks.storage.delete(key)})}}));
vi.mock('./export-watermark-picker',()=>({ExportWatermarkPicker:()=>null}));
vi.mock('./export-batch-panel',()=>({ExportBatchPanel:()=>null}));
vi.mock('./download-job-panel',()=>({DownloadJobPanel:()=>null}));
vi.mock('./download-job-store',()=>({DownloadJobStore:class {attempt:unknown=null;read=async()=>null;readAttempt=async()=>null;begin=async(snapshot:string,preset:string)=>this.attempt??(this.attempt={snapshot,preset,key:crypto.randomUUID()});complete=async()=>{this.attempt=null};refuse=async()=>false;write=async()=>{};}}));
vi.mock('./preset-metadata-editor',()=>({PresetMetadataEditor:()=>null}));
import Exports from '../screens/exports/index';

const preset=(name:string)=>({id:`00000000-0000-4000-8000-${name==='Old'?'000000000001':'000000000002'}`,name,revision:'1',config:{format:'jpeg',icc_id:'srgb_v1',metadata_policy:'strip'}});
beforeEach(()=>{mocks.storage.clear();mocks.auth={origin:'https://example.test',client:()=>mocks.first};mocks.presets.mockReset();mocks.createPreset.mockReset();mocks.startDownload.mockReset();});
afterEach(cleanup);

it('removes a previous user’s presets before loading the same tenant with a new client',async()=>{
 mocks.presets.mockResolvedValueOnce({items:[preset('Old')],next_cursor:null}).mockResolvedValueOnce({items:[preset('New')],next_cursor:null});
 const view=render(<Exports/>);
 await screen.findByText('Old');
 mocks.auth={origin:'https://example.test',client:()=>mocks.second};
 view.rerender(<Exports/>);
 expect(screen.queryByText('Old')).toBeNull();
 await screen.findByText('New');
 expect(mocks.presets).toHaveBeenCalledTimes(2);
});

it('ignores a late preset response from a previous authenticated client',async()=>{
 let finishOld!:(value:{items:ReturnType<typeof preset>[];next_cursor:null})=>void;
 mocks.presets.mockImplementationOnce(()=>new Promise(resolve=>{finishOld=resolve})).mockResolvedValueOnce({items:[preset('New')],next_cursor:null});
 const view=render(<Exports/>);
 expect(mocks.presets).toHaveBeenCalledTimes(1);
 mocks.auth={origin:'https://example.test',client:()=>mocks.second};
 view.rerender(<Exports/>);
 await screen.findByText('New');
 await act(async()=>finishOld({items:[preset('Old')],next_cursor:null}));
 expect(screen.queryByText('Old')).toBeNull();
 expect(screen.getByText('New')).toBeTruthy();
});

it('retries an uncertain preset creation with the original idempotency key',async()=>{
 mocks.presets.mockResolvedValue({items:[],next_cursor:null});
 mocks.createPreset.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(preset('New'));
 render(<Exports/>);
 await screen.findByText('No export presets');
 fireEvent.click(screen.getByText('Create'));
 await screen.findByText('Unable to create preset');
 const retry=screen.getByText('Retry preset creation');await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await screen.findByText('New');
 expect(mocks.createPreset).toHaveBeenCalledTimes(2);
 expect(mocks.createPreset.mock.calls[0].slice(0,3)).toEqual(mocks.createPreset.mock.calls[1].slice(0,3));
 expect(mocks.createPreset.mock.calls[0][2]).toEqual(expect.any(String));
});

it('retries an uncertain download start with the original idempotency key',async()=>{
 mocks.presets.mockResolvedValue({items:[preset('Old')],next_cursor:null});
 mocks.startDownload.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({download_job_id:'00000000-0000-4000-8000-000000000099',status:'queued'});
 render(<Exports/>);
 await screen.findByText('Old');
 await clickWhenEnabled(screen.getByText('Old')); 
 await screen.findByText('Unable to start download job');
 await clickWhenEnabled(screen.getByText('Old')); 
 await waitFor(()=>expect(screen.getByText(/Download job 00000000 is Queued/)).toBeTruthy());
 expect(mocks.startDownload).toHaveBeenCalledTimes(2);
 expect(mocks.startDownload.mock.calls[0].slice(0,3)).toEqual(mocks.startDownload.mock.calls[1].slice(0,3));
});

it('loads later preset pages and keeps every preset available',async()=>{
 mocks.presets.mockResolvedValueOnce({items:[preset('Old')],next_cursor:'page-2'}).mockResolvedValueOnce({items:[preset('New')],next_cursor:null});
 render(<Exports/>);await screen.findByText('Old');
 fireEvent.click(screen.getByText('Load more presets'));
 await screen.findByText('New');
 expect(screen.getByText('Old')).toBeTruthy();
 expect(mocks.presets.mock.calls[1][0]).toBe('page-2');
 expect(screen.queryByText('Load more presets')).toBeNull();
});

it('refuses a repeated cursor without adding the second page',async()=>{
 mocks.presets.mockResolvedValueOnce({items:[preset('Old')],next_cursor:'page-2'}).mockResolvedValueOnce({items:[preset('New')],next_cursor:'page-2'});
 render(<Exports/>);await screen.findByText('Old');
 fireEvent.click(screen.getByText('Load more presets'));
 await screen.findByRole('alert');
 expect(screen.queryByText('New')).toBeNull();
 expect(screen.getByText('Old')).toBeTruthy();
});

it('refuses a duplicate preset identity from a later page',async()=>{
 mocks.presets.mockResolvedValueOnce({items:[preset('Old')],next_cursor:'page-2'}).mockResolvedValueOnce({items:[preset('Old')],next_cursor:null});
 render(<Exports/>);await screen.findByText('Old');
 fireEvent.click(screen.getByText('Load more presets'));
 await screen.findByRole('alert');
 expect(screen.getAllByText('Old')).toHaveLength(1);
});

it('forwards cancellation to preset authoring and ignores accepted response after account switch',async()=>{
 let finish!:(value:ReturnType<typeof preset>)=>void;
 mocks.presets.mockResolvedValue({items:[preset('Old')],next_cursor:null});
 mocks.createPreset.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve}));
 const view=render(<Exports/>);await screen.findByText('Old');
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Create'})));
 await clickWhenEnabled(screen.getByText('Create'));await waitFor(()=>expect(mocks.createPreset).toHaveBeenCalledTimes(1));
 const signal=mocks.createPreset.mock.calls[0][3] as AbortSignal;
 mocks.auth={origin:'https://example.test',client:()=>mocks.second};view.rerender(<Exports/>);
 expect(signal.aborted).toBe(true);
 await act(async()=>finish(preset('New')));
 expect(screen.queryByText('New')).toBeNull();
});
it('cancels uncertain preset creation and retries its same frozen body and key',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.presets.mockResolvedValue({items:[],next_cursor:null});mocks.createPreset.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce(preset('New'));
 render(<Exports/>);await screen.findByText('No export presets');
 await waitFor(()=>expectEnabled(screen.getByText('Create')));
 await clickWhenEnabled(screen.getByText('Create'));await waitFor(()=>expect(mocks.createPreset).toHaveBeenCalledTimes(1));
 const original=mocks.createPreset.mock.calls[0];fireEvent.click(screen.getByText('Stop loading'));
 await act(async()=>finish(preset('Old')));expect(screen.queryByText('Old')).toBeNull();
 fireEvent.click(screen.getByText('Retry preset creation'));await screen.findByText('New');
 expect(mocks.createPreset.mock.calls[1].slice(0,3)).toEqual(original.slice(0,3));
});
it('withdraws all export projections on definitive preset access loss and recovers only through readonly retry',async()=>{
 const {APIError}=await import('@photoeditor/api-client');
 mocks.presets.mockResolvedValueOnce({items:[preset('Old')],next_cursor:'next'}).mockRejectedValueOnce(new APIError(403,'DENIED')).mockResolvedValueOnce({items:[preset('New')],next_cursor:null});
 render(<Exports/>);await screen.findByText('Old');fireEvent.click(screen.getByText('Load more presets'));
 await screen.findByRole('alert');expect(screen.queryByText('Old')).toBeNull();expect(screen.queryByText('Create')).toBeNull();
 fireEvent.click(screen.getByText('Retry loading'));await screen.findByText('New');expect(mocks.createPreset).not.toHaveBeenCalled();expect(mocks.startDownload).not.toHaveBeenCalled();
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
