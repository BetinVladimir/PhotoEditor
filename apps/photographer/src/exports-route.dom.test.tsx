// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';

const mocks=vi.hoisted(()=>{
 const first={request:vi.fn()},second={request:vi.fn()};
 return {first,second,auth:{origin:'https://example.test',client:()=>first},params:{tenant:'tenant-a',snapshot:'snapshot-a'},presets:vi.fn(),createPreset:vi.fn(),startDownload:vi.fn()};
});
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {presets=mocks.presets;createPreset=mocks.createPreset;startDownload=mocks.startDownload;}}));
vi.mock('./preset-metadata-editor',()=>({PresetMetadataEditor:()=>null}));
import Exports from '../app/exports/index';

const preset=(name:string)=>({id:`00000000-0000-4000-8000-${name==='Old'?'000000000001':'000000000002'}`,name,revision:'1',config:{format:'jpeg',icc_id:'srgb_v1',metadata_policy:'strip'}});
beforeEach(()=>{mocks.auth={origin:'https://example.test',client:()=>mocks.first};mocks.presets.mockReset();mocks.createPreset.mockReset();mocks.startDownload.mockReset();});
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
 fireEvent.click(screen.getByText('Create'));
 await screen.findByText('New');
 expect(mocks.createPreset).toHaveBeenCalledTimes(2);
 expect(mocks.createPreset.mock.calls[0]).toEqual(mocks.createPreset.mock.calls[1]);
 expect(mocks.createPreset.mock.calls[0][2]).toEqual(expect.any(String));
});

it('retries an uncertain download start with the original idempotency key',async()=>{
 mocks.presets.mockResolvedValue({items:[preset('Old')],next_cursor:null});
 mocks.startDownload.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({download_job_id:'00000000-0000-4000-8000-000000000099',status:'queued'});
 render(<Exports/>);
 await screen.findByText('Old');
 fireEvent.click(screen.getByText('Old'));
 await screen.findByText('Unable to start download job');
 fireEvent.click(screen.getByText('Old'));
 await waitFor(()=>expect(screen.getByText(/Download job 00000000 is queued/)).toBeTruthy());
 expect(mocks.startDownload).toHaveBeenCalledTimes(2);
 expect(mocks.startDownload.mock.calls[0]).toEqual(mocks.startDownload.mock.calls[1]);
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
