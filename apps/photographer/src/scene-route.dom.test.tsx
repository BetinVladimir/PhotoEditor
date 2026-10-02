// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
import type {SceneBoard} from './scene-board';
const mocks=vi.hoisted(()=>({params:{id:'session',tenant:'tenant',analysis:'first'},read:vi.fn(),session:vi.fn(),collection:vi.fn(),snapshot:vi.fn(),analyze:vi.fn(),push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
const auth={client:()=>({})};vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
vi.mock('./locale-picker',()=>({LocalePicker:()=>null}));
vi.mock('./owner-api',()=>({OwnerAPI:class{tenant:string;constructor(_client:unknown,tenant:string){this.tenant=tenant;}sceneBoard=mocks.read;shootSession=mocks.session;collection=mocks.collection;selectionSnapshot=mocks.snapshot;analyzeSession=mocks.analyze;}}));
import SessionScenes from '../app/sessions/[id]/scenes';
afterEach(()=>{cleanup();vi.clearAllMocks();mocks.params={id:'session',tenant:'tenant',analysis:'first'};});
const board:SceneBoard={snapshotId:'snapshot',snapshotDigest:'a'.repeat(64),policySha:'b'.repeat(64),groups:[]};
it('does not display a late board after navigating to another analysis',async()=>{
 let release!:(value:SceneBoard)=>void;const late=new Promise<SceneBoard>(resolve=>release=resolve);mocks.read.mockReturnValueOnce(late).mockRejectedValueOnce(new Error('denied'));
 const view=render(<SessionScenes/>);await waitFor(()=>expect(mocks.read).toHaveBeenCalledTimes(1));const signal=mocks.read.mock.calls[0][2] as AbortSignal;
 mocks.params={...mocks.params,analysis:'second'};view.rerender(<SessionScenes/>);await screen.findByText(en.scene_load_failed);expect(signal.aborted).toBe(true);
 await act(async()=>release(board));expect(screen.queryByText(en.scene_board_title)).toBeNull();
});
it('renders a loaded board only for the current authorized scope',async()=>{
 mocks.read.mockResolvedValue(board);render(<SessionScenes/>);await screen.findByText(en.scene_board_title);expect(mocks.read).toHaveBeenCalledWith('first','session',expect.any(AbortSignal));
});

const frame={ordinal:0,versionId:'version',sourceSha:'a'.repeat(64)};
const selectable:SceneBoard={...board,groups:[{id:'group',ordinals:[0],frames:[frame],reference:frame,confidence:1,corrections:{artifact_id:'correction',sha256:'a'.repeat(64)},outliers:null}]};
function preparation(){mocks.read.mockResolvedValue(selectable);mocks.session.mockResolvedValue({state:'open',revision:'2',collection_id:'collection',policy:{schema_version:1,camera_offsets:[]}});mocks.collection.mockResolvedValue({id:'collection',revision:'3'});mocks.snapshot.mockResolvedValue({id:'snapshot'});}
it('retries the same admitted request/key without creating a second snapshot',async()=>{
 preparation();mocks.analyze.mockRejectedValueOnce(new Error('lost response')).mockResolvedValueOnce({run_id:'new-run'});
 render(<SessionScenes/>);await screen.findByText(en.scene_board_title);fireEvent.click(screen.getByLabelText(en.scene_reference_select+' 1'));
 await screen.findByText(en.scene_retry_analysis);const first=mocks.analyze.mock.calls[0];fireEvent.click(screen.getByText(en.scene_retry_analysis));await waitFor(()=>expect(mocks.push).toHaveBeenCalledTimes(1));
 const second=mocks.analyze.mock.calls[1];expect(second[1]).toEqual(first[1]);expect(second[2]).toBe(first[2]);expect(mocks.snapshot).toHaveBeenCalledTimes(1);expect(mocks.session).toHaveBeenCalledTimes(1);
});
it('ignores a late admission ticket from a previous analysis scope',async()=>{
 preparation();let release!:(value:unknown)=>void;mocks.analyze.mockReturnValue(new Promise(resolve=>release=resolve));
 const view=render(<SessionScenes/>);await screen.findByText(en.scene_board_title);fireEvent.click(screen.getByLabelText(en.scene_reference_select+' 1'));await waitFor(()=>expect(mocks.analyze).toHaveBeenCalledTimes(1));
 mocks.params={...mocks.params,analysis:'second'};view.rerender(<SessionScenes/>);await act(async()=>release({run_id:'old-run'}));expect(mocks.push).not.toHaveBeenCalled();expect((mocks.analyze.mock.calls[0][3] as AbortSignal).aborted).toBe(true);
});
it('clears displayed metadata when a reference mutation reveals revoked access',async()=>{
 preparation();mocks.analyze.mockRejectedValue(new APIError(403,'FORBIDDEN'));
 render(<SessionScenes/>);await screen.findByText(en.scene_board_title);fireEvent.click(screen.getByLabelText(en.scene_reference_select+' 1'));await screen.findByText(en.scene_access_denied);expect(screen.queryByText(en.scene_board_title)).toBeNull();expect(screen.queryByText(en.scene_retry_analysis)).toBeNull();
});
it('releases a definitively rejected request so the user can reload before a new choice',async()=>{
 preparation();mocks.analyze.mockRejectedValue(new APIError(409,'ANALYSIS_CONFLICT'));
 render(<SessionScenes/>);await screen.findByText(en.scene_board_title);fireEvent.click(screen.getByLabelText(en.scene_reference_select+' 1'));await screen.findByText(en.scene_reference_conflict);
 expect(screen.queryByText(en.scene_retry_analysis)).toBeNull();expect(screen.getByText(en.scene_reload).closest('[role="button"]')?.getAttribute('aria-disabled')).not.toBe('true');expect(mocks.analyze).toHaveBeenCalledTimes(1);
});
