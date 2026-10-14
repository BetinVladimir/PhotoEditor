// @vitest-environment jsdom
import {afterEach,beforeEach,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
const mocks=vi.hoisted(()=>({replace:vi.fn(),values:new Map<string,string>(),failWrite:false,api:{collection:vi.fn(),me:vi.fn(),styles:vi.fn(),styleVersions:vi.fn(),apply:vi.fn()},auth:{origin:'https://studio.test',client:()=>({})}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({router:{replace:mocks.replace},useLocalSearchParams:()=>({id:'00000000-0000-4000-8000-000000000004',tenant:'00000000-0000-4000-8000-000000000002'})}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {constructor(){return mocks.api}}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>mocks.values.get(k)??null,setItem:async(k:string,v:string)=>{if(mocks.failWrite)throw new Error('disk unavailable');mocks.values.set(k,v)},removeItem:async(k:string)=>{mocks.values.delete(k)},getAllKeys:async()=>[...mocks.values.keys()]}}));
import Apply from '../app/collections/[id]/apply';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{
 Object.defineProperty(navigator,'locks',{configurable:true,value:{request:async(_key:string,_options:unknown,action:()=>Promise<unknown>)=>action()}});
 mocks.api.me.mockResolvedValue({id:id(1)});mocks.api.collection.mockResolvedValue({id:id(4),tenant_id:id(2),project_id:id(3),revision:'7',name:'Original'});mocks.api.styles.mockResolvedValue({items:[{id:id(10),name:'Studio'}]});mocks.api.styleVersions.mockResolvedValue({items:[{id:id(5),version_no:1,approval_status:'accepted'}]});
});
afterEach(()=>{cleanup();mocks.values.clear();mocks.failWrite=false;vi.resetAllMocks()});
async function submit(){fireEvent.click(await screen.findByText('Studio'));fireEvent.click(await screen.findByText('Version 1'));fireEvent.click(screen.getByText('Start processing'))}
it('restores frozen pending request after remount despite changed source revision',async()=>{
 mocks.api.apply.mockRejectedValueOnce(new Error('reply lost')).mockResolvedValueOnce({run_id:id(7),output_collection_id:id(8),status:'queued'});
 const view=render(<Apply/>);await submit();await screen.findByText('Unable to confirm apply run. Retry sends the same request.');const first=mocks.api.apply.mock.calls[0];
 expect([...mocks.values.values()].some(value=>value.includes(first[4]))).toBe(true);view.unmount();
 mocks.api.collection.mockResolvedValue({id:id(4),tenant_id:id(2),project_id:id(3),revision:'99',name:'Changed'});
 render(<Apply/>);await screen.findByText('Studio');fireEvent.click(screen.getByText('Start processing'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));expect(mocks.api.apply.mock.calls[1].slice(0,5)).toEqual(first.slice(0,5));expect(first[0].revision).toBe('7');
});
it('restores confirmed ticket without another admission and permits acknowledged-only new operation',async()=>{
 mocks.api.apply.mockResolvedValue({run_id:id(7),output_collection_id:id(8),status:'queued'});
 const first=render(<Apply/>);await submit();await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(1));first.unmount();
 const second=render(<Apply/>);fireEvent.click(await screen.findByText('Open existing run'));await waitFor(()=>expect(mocks.replace).toHaveBeenCalledTimes(2));expect(mocks.api.apply).toHaveBeenCalledTimes(1);second.unmount();
 render(<Apply/>);fireEvent.click(await screen.findByText('Start another operation'));await screen.findByText('Start processing');expect([...mocks.values.keys()].filter(key=>key.startsWith('photo.collection-apply.v1:'))).toHaveLength(0);
});
it('refuses HTTP admission when durable pending write fails',async()=>{
 mocks.failWrite=true;render(<Apply/>);await submit();await screen.findByText('Unable to confirm apply run. Retry sends the same request.');expect(mocks.api.apply).not.toHaveBeenCalled();expect(mocks.replace).not.toHaveBeenCalled();
});
