// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup,act} from '@testing-library/react';
const mocks=vi.hoisted(()=>({locale:'en' as 'en'|'ru'|'bg',replace:vi.fn(),api:{collection:vi.fn(),me:vi.fn(),styles:vi.fn(),styleVersions:vi.fn(),apply:vi.fn()},auth:{client:()=>({})}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({router:{replace:mocks.replace},useLocalSearchParams:()=>({id:'collection',tenant:'tenant'})}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {constructor(){return mocks.api}}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./collection-apply-store',()=>({CollectionApplyStore:class {async read(){return null}async write(){}async clear(){}async resume(record:unknown,send:(value:unknown)=>Promise<unknown>){return {...record as object,accepted:await send(record)}}}}));
vi.mock('./locale-context',async()=>{const en=(await import('../../../locales/en/common.json')).default,ru=(await import('../../../locales/ru/common.json')).default,bg=(await import('../../../locales/bg/common.json')).default;return {useLocale:()=>({t:(key:keyof typeof en)=>({en,ru,bg})[mocks.locale][key]})}});
import Apply from '../app/collections/[id]/apply';
afterEach(()=>{cleanup();mocks.locale="en";vi.resetAllMocks()});
function setup(){mocks.api.me.mockResolvedValue({id:'user'});mocks.api.collection.mockResolvedValue({id:'collection',project_id:'project',revision:'1',name:'Original'});mocks.api.styles.mockResolvedValue({items:[{id:'first',name:'First'},{id:'second',name:'Second'}]});return render(<Apply/>)}
it('ignores late versions from a previously chosen profile',async()=>{
 let resolve!:(value:unknown)=>void;
 mocks.api.styleVersions.mockImplementation((id:string)=>id==='first'?new Promise(r=>resolve=r):Promise.resolve({items:[{id:'new',version_no:2,approval_status:'accepted'}]}));
 setup();fireEvent.click(await screen.findByText('First'));fireEvent.click(screen.getByText('Second'));
 await screen.findByText('Version 2');
 await act(async()=>resolve({items:[{id:'old',version_no:1,approval_status:'accepted'}]}));
 expect(screen.queryByText('Version 1')).toBeNull();expect(screen.getByText('Version 2')).toBeTruthy();expect(mocks.api.apply).not.toHaveBeenCalled();
});
it('clears selected versions when another profile fails to load',async()=>{
 mocks.api.styleVersions.mockResolvedValueOnce({items:[{id:'old',version_no:1,approval_status:'accepted'}]}).mockRejectedValueOnce(new Error('offline'));
 setup();fireEvent.click(await screen.findByText('First'));fireEvent.click(await screen.findByText('Version 1'));fireEvent.click(screen.getByText('Second'));
 await screen.findByText('Unable to load style versions');expect(screen.queryByText('Version 1')).toBeNull();fireEvent.click(screen.getByText('Start processing'));expect(mocks.api.apply).not.toHaveBeenCalled();
});
it('does not fetch profiles after collection load completes following unmount',async()=>{
 let resolve!:(value:unknown)=>void;mocks.api.collection.mockImplementation(()=>new Promise(r=>resolve=r));
 // Configure without setup, which installs an immediately resolved collection.
 const view=render(<Apply/>);await waitFor(()=>expect(mocks.api.collection).toHaveBeenCalled());view.unmount();
 await act(async()=>resolve({id:'collection',project_id:'project',name:'Old',revision:'1'}));expect(mocks.api.styles).not.toHaveBeenCalled();
});

it('retries an uncertain Apply with the same frozen request and key',async()=>{
 mocks.api.styleVersions.mockResolvedValue({items:[{id:'old',version_no:1,approval_status:'accepted'}]});
 mocks.api.apply.mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce({run_id:'run',output_collection_id:'output',status:'queued'});
 setup();fireEvent.click(await screen.findByText('First'));fireEvent.click(await screen.findByText('Version 1'));fireEvent.click(screen.getByText('Start processing'));
 await screen.findByText('Unable to confirm apply run. Retry sends the same request.');
 const first=mocks.api.apply.mock.calls[0];expect(first[4]).toBeTruthy();
 fireEvent.click(screen.getByText('Second'));expect(mocks.api.styleVersions).toHaveBeenCalledTimes(1);
 fireEvent.click(screen.getByText('Start processing'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));
 expect(mocks.api.apply.mock.calls[1]).toEqual(first);
 fireEvent.click(screen.getByText('Open existing run'));expect(mocks.api.apply).toHaveBeenCalledTimes(2);
});

it.each(['ru','bg'] as const)('localizes Apply controls and uncertain recovery in %s',async locale=>{
 mocks.locale=locale;const catalog=(await import(`../../../locales/${locale}/common.json`)).default;
 mocks.api.styleVersions.mockResolvedValue({items:[{id:'old',version_no:1,approval_status:'accepted'}]});mocks.api.apply.mockRejectedValue(new Error('reply lost'));
 setup();expect(screen.getByText(catalog.collection_apply_title)).toBeTruthy();fireEvent.click(await screen.findByText('First'));fireEvent.click(await screen.findByText(catalog.collection_apply_version.replace('{number}','1')));fireEvent.click(screen.getByText(catalog.collection_apply_submit));await screen.findByText(catalog.collection_apply_uncertain);expect(screen.getByLabelText(catalog.collection_apply_name)).toBeTruthy();
});

it('rejects overlong Unicode name before freezing and permits correcting it',async()=>{
 mocks.api.styleVersions.mockResolvedValue({items:[{id:'old',version_no:1,approval_status:'accepted'}]});mocks.api.apply.mockResolvedValue({run_id:'run',output_collection_id:'output',status:'queued'});
 setup();fireEvent.click(await screen.findByText('First'));fireEvent.click(await screen.findByText('Version 1'));
 const name=screen.getByLabelText('Output collection name');fireEvent.change(name,{target:{value:'😀'.repeat(121)}});fireEvent.click(screen.getByText('Start processing'));await screen.findByText('Output collection name must be at most 120 characters');expect(mocks.api.apply).not.toHaveBeenCalled();expect((name as HTMLInputElement).readOnly).toBe(false);
 fireEvent.change(name,{target:{value:'😀'.repeat(120)}});fireEvent.click(screen.getByText('Start processing'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));expect(mocks.api.apply.mock.calls[0][3]).toBe('😀'.repeat(120));
});

it('includes style versions beyond the first page',async()=>{
 mocks.api.styleVersions.mockImplementation((_id:string,cursor?:string)=>Promise.resolve(cursor?{items:[{id:'later',version_no:2,approval_status:'accepted'}]}:{items:[{id:'first-page',version_no:1,approval_status:'accepted'}],next_cursor:'next'}));
 setup();fireEvent.click(await screen.findByText('First'));await screen.findByText('Version 2');expect(screen.getByText('Version 1')).toBeTruthy();expect(mocks.api.styleVersions.mock.calls.map(call=>call[1])).toEqual([undefined,'next']);
});

it('aborts collection discovery and selected-profile reads when leaving or changing selection',async()=>{
 let resolve!:(value:unknown)=>void;mocks.api.styleVersions.mockImplementation(()=>new Promise(r=>resolve=r));const view=setup();fireEvent.click(await screen.findByText('First'));const first=mocks.api.styleVersions.mock.calls[0][2] as AbortSignal;fireEvent.click(screen.getByText('Second'));expect(first.aborted).toBe(true);const second=mocks.api.styleVersions.mock.calls[1][2] as AbortSignal;const collectionSignal=mocks.api.collection.mock.calls[0][1] as AbortSignal;view.unmount();expect(second.aborted).toBe(true);expect(collectionSignal.aborted).toBe(true);
 await act(async()=>resolve({items:[]}));expect(mocks.replace).not.toHaveBeenCalled();
});
