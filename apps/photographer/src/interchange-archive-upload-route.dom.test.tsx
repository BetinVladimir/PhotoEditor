// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor,act} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const state=vi.hoisted(()=>({params:{tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002',format:'photoeditor_bundle'},client:{request:vi.fn()},me:vi.fn(),prepare:vi.fn(),replace:vi.fn()}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>state.params,router:{replace:state.replace}}));
vi.mock('./auth-context',()=>{const auth={origin:'https://archive-route.test',client:()=>state.client};return {useAuth:()=>auth};});
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./owner-api',()=>({OwnerAPI:class{me=state.me;}}));
vi.mock('./interchange-archive-picker',()=>({prepareArchiveCache:state.prepare,pickInterchangeArchive:vi.fn()}));
vi.mock('./interchange-archive-screen',()=>({InterchangeArchiveScreen:({picker}:{picker:unknown})=> {expect(picker).toBeTypeOf('function');expect(picker).not.toBe(state.prepare);return <div>archive-screen</div>;}}));
import ArchiveUploadRoute, {archiveUploadRouteScope} from '../screens/interchange/upload';
afterEach(()=>{cleanup();vi.resetAllMocks();});
it('prepares owned cache before loading principal and exposing upload screen',async()=>{
 const events:string[]=[];state.prepare.mockImplementation(()=>{events.push('cache');});state.me.mockImplementation(async()=>{events.push('me');return {id:'00000000-0000-4000-8000-000000000009'};});
 render(<ArchiveUploadRoute/>);await screen.findByText('archive-screen');expect(events).toEqual(['cache','me']);expect(state.replace).not.toHaveBeenCalled();
});
it('does not start identity request or upload when cache preparation fails',async()=>{
 state.prepare.mockImplementation(()=>{throw new Error('disk');});render(<ArchiveUploadRoute/>);await screen.findByRole('alert');
 expect(state.me).not.toHaveBeenCalled();expect(screen.queryByText('archive-screen')).toBeNull();
});
it('cancels identity lookup and does not mount upload after route teardown',async()=>{
 let signal:AbortSignal|undefined,resolve!:(value:{id:string})=>void;
 state.me.mockImplementation(async(current:AbortSignal)=>{signal=current;return new Promise(done=>{resolve=done;});});
 const view=render(<ArchiveUploadRoute/>);await waitFor(()=>expect(signal).toBeDefined());view.unmount();expect(signal!.aborted).toBe(true);
 await act(async()=>{resolve({id:'00000000-0000-4000-8000-000000000009'});});expect(screen.queryByText('archive-screen')).toBeNull();expect(state.replace).not.toHaveBeenCalled();
});

it('explicitly retries failed cache preparation before exposing upload',async()=>{
 state.prepare.mockImplementationOnce(()=>{throw new Error('busy');});state.me.mockResolvedValue({id:'00000000-0000-4000-8000-000000000009'});
 render(<ArchiveUploadRoute/>);await screen.findByRole('alert');expect(state.prepare).toHaveBeenCalledOnce();expect(state.me).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('retry'));await screen.findByText('archive-screen');
 expect(state.prepare).toHaveBeenCalledTimes(2);expect(state.me).toHaveBeenCalledOnce();expect(screen.queryByRole('alert')).toBeNull();expect(state.replace).not.toHaveBeenCalled();
});
it('repeats principal lookup only after an explicit retry',async()=>{
 state.me.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({id:'00000000-0000-4000-8000-000000000009'});
 render(<ArchiveUploadRoute/>);await screen.findByRole('alert');expect(state.me).toHaveBeenCalledOnce();
 fireEvent.click(screen.getByText('retry'));await screen.findByText('archive-screen');expect(state.me).toHaveBeenCalledTimes(2);expect(state.prepare).toHaveBeenCalledTimes(2);
});

it.each(['photoeditor_bundle','xmp','captureone_sidecar'])('preserves explicit %s in upload route scope',format=>{
 const tenant='00000000-0000-4000-8000-000000000001',project='00000000-0000-4000-8000-000000000002';
 expect(archiveUploadRouteScope({tenant,project,format})).toEqual({tenant,project,format});
});
it.each([undefined,'XMP','cos','xmp ','unknown'])('refuses missing or unknown upload format %s',format=>{
 expect(()=>archiveUploadRouteScope({tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002',format})).toThrow('INTERCHANGE_ARCHIVE_ROUTE_INVALID');
});

it('cancelled identity can retry before a late prior principal resolves',async()=>{
 let finish!:(value:{id:string})=>void;state.me.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({id:'00000000-0000-4000-8000-000000000009'});
 render(<ArchiveUploadRoute/>);fireEvent.click(screen.getByText('read_cancel'));await screen.findByRole('alert');fireEvent.click(screen.getByText('retry'));await screen.findByText('archive-screen');
 await act(async()=>finish({id:'00000000-0000-4000-8000-000000000008'}));expect(state.me).toHaveBeenCalledTimes(2);expect(state.replace).not.toHaveBeenCalled();
});
