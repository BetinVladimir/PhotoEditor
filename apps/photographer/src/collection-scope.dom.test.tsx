// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor,act} from '@testing-library/react';
const mocks=vi.hoisted(()=>({params:{id:'one',tenant:'tenant'},request:vi.fn(),snapshot:vi.fn(),sheet:vi.fn(),push:vi.fn(),media:vi.fn(),moderate:vi.fn()}));
vi.mock('./gallery-comparison',()=>({GalleryComparison:()=>null}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
const client={request:mocks.request};const auth={client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {mediaLinks=mocks.media;selectionSnapshot=mocks.snapshot;contactSheet=mocks.sheet;moderateApplyItem=mocks.moderate;}}));
import CollectionDetail from '../app/collections/[id]/index';
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(r=>resolve=r);return {promise,resolve};}
afterEach(()=>{cleanup();vi.clearAllMocks();mocks.params={id:'one',tenant:'tenant'};});
function load(){mocks.request.mockImplementation((path:string)=>Promise.resolve(path.includes('/items?')?{revision:'1',items:[{collection_id:mocks.params.id,tags:[],id:'item',asset_id:'asset',asset_version_id:'version',position:0,moderation_state:'pending_review'}]}:{id:mocks.params.id,name:mocks.params.id,state:'ready',revision:'1',source_run_id:'run'}));mocks.media.mockResolvedValue({items:[]});}
it('changing collections suppresses late previews and resets visible collection',async()=>{
 load();const preview=deferred<{items:{variant:string;url:string}[]}>();mocks.media.mockReturnValueOnce(preview.promise);
 const view=render(<CollectionDetail/>);await screen.findByText('one');
 mocks.params={id:'two',tenant:'tenant'};view.rerender(<CollectionDetail/>);await screen.findByText('two');
 await act(async()=>preview.resolve({items:[{variant:'thumb640',url:'https://old.test/preview'}]}));
 expect(screen.queryByText('one')).toBeNull();expect(document.querySelector('img[src="https://old.test/preview"]')).toBeNull();
});
it('a late snapshot cannot submit a contact sheet for the previous collection',async()=>{
 load();const snapshot=deferred<{id:string}>();mocks.snapshot.mockReturnValue(snapshot.promise);
 const view=render(<CollectionDetail/>);await screen.findByText('one');
 fireEvent.click(screen.getByText('Create contact sheet'));fireEvent.click(screen.getByText('Creating contact sheet…'));expect(mocks.snapshot).toHaveBeenCalledTimes(1);
 mocks.params={id:'two',tenant:'tenant'};view.rerender(<CollectionDetail/>);await screen.findByText('two');
 await act(async()=>snapshot.resolve({id:'old-snapshot'}));expect(mocks.sheet).not.toHaveBeenCalled();expect(mocks.push).not.toHaveBeenCalled();
});
it('a committed old contact sheet cannot navigate the new collection',async()=>{
 load();mocks.snapshot.mockResolvedValue({id:'snapshot'});const sheet=deferred<{run_id:string}>();mocks.sheet.mockReturnValue(sheet.promise);
 const view=render(<CollectionDetail/>);await screen.findByText('one');fireEvent.click(screen.getByText('Create contact sheet'));await waitFor(()=>expect(mocks.sheet).toHaveBeenCalledTimes(1));
 mocks.params={id:'two',tenant:'tenant'};view.rerender(<CollectionDetail/>);await screen.findByText('two');
 await act(async()=>sheet.resolve({run_id:'old-run'}));expect(mocks.push).not.toHaveBeenCalled();
});
it('reloads authoritative metadata after failed moderation without retrying the mutation',async()=>{
 load();mocks.moderate.mockRejectedValue(new Error('uncertain transport'));
 // Add the exact collection membership required by moderation preflight.
 const original=mocks.request.getMockImplementation()!;
 mocks.request.mockImplementation(async(path:string,...rest:unknown[])=>{const value=await original(path,...rest);return path.includes('/items?')?{revision:value.revision,items:value.items.map((item:object)=>({...item,collection_id:'one'}))}:value;});
 render(<CollectionDetail/>);await screen.findByText('one');await waitFor(()=>expect(mocks.media).toHaveBeenCalled());
 fireEvent.click(screen.getByLabelText('Accept photo 1'));await screen.findByText('Some decisions were not saved. Reload the collection before retrying.');expect(mocks.moderate).toHaveBeenCalledTimes(1);
 expect(screen.queryByLabelText('Accept photo 1')).toBeNull();
 const count=mocks.request.mock.calls.length;fireEvent.click(screen.getByText('Reload collection'));await screen.findByText('one');await waitFor(()=>expect(mocks.request.mock.calls.length).toBe(count+2));expect(mocks.moderate).toHaveBeenCalledTimes(1);expect(screen.getByLabelText('Accept photo 1')).toBeDefined();
});
