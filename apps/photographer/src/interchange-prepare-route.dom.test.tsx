// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {cleanup,render,screen} from '@testing-library/react';
const mocks=vi.hoisted(()=>({params:{} as Record<string,string>,me:vi.fn(),boundary:vi.fn(),replace:vi.fn(),client:{request:vi.fn()}}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({useLocalSearchParams:()=>mocks.params,router:{replace:mocks.replace}}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async()=>null,setItem:async()=>{},removeItem:async()=>{}}}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
vi.mock('./auth-context',()=>({useAuth:()=>({origin:'https://prepare.test',client:()=>mocks.client})}));
vi.mock('./owner-api',()=>({OwnerAPI:class{me=mocks.me;}}));
vi.mock('./interchange-snapshot-screen',()=>({InterchangeSnapshotScreen:(props:any)=>{props.journal.assertScope(props.tenant,props.project,props.request);mocks.boundary(props);return 'Snapshot boundary';}}));
import Route,{editorInterchangeSnapshotRequest} from '../app/interchange/prepare';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
beforeEach(()=>{mocks.params={tenant:id(1),project:id(2),version:id(3),source:id(4),source_sha:'a'.repeat(64),canvas:'b'.repeat(64),recipe:id(5),recipe_sha:'c'.repeat(64)};mocks.me.mockReset().mockResolvedValue({id:id(9)});mocks.boundary.mockReset();mocks.replace.mockReset();});
afterEach(cleanup);
it('pins editor source and recipe and hands acknowledged snapshot to bundle creation',async()=>{
 render(<Route/>);await screen.findByText('Snapshot boundary');const props=mocks.boundary.mock.lastCall![0];
 expect(props.request.items[0]).toMatchObject({binding:{asset_version_id:id(3)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)}});
 props.onPrepared({id:id(6)});expect(mocks.replace).toHaveBeenCalledWith({pathname:'/interchange/format',params:{tenant:id(1),project:id(2),snapshot:id(6)}});
});
it('rejects malformed source before identity IO',async()=>{
 mocks.params.source_sha='invalid';render(<Route/>);await screen.findByRole('alert');expect(mocks.me).not.toHaveBeenCalled();
});
it('does not infer a recipe when route pin is missing',()=>{
 delete mocks.params.recipe;expect(()=>editorInterchangeSnapshotRequest(mocks.params)).toThrow();
});
