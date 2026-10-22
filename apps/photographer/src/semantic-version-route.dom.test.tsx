// @vitest-environment jsdom
import {clickWhenEnabled} from './test/ui-readiness';
import React from 'react';
import {userProfile} from './test/user-profile';
import {afterEach,it,expect,vi} from 'vitest';
import {cleanup,render,screen,fireEvent,waitFor} from '@testing-library/react';
const mocks=vi.hoisted(()=>({push:vi.fn(),semanticTargets:vi.fn(),materializeSemanticShownVersion:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>{const rows=new Map<string,string>();return {default:{getItem:async(key:string)=>rows.get(key)??null,setItem:async(key:string,value:string)=>{rows.set(key,value)},removeItem:async(key:string)=>{rows.delete(key)}}}});
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({actor:'88888888-8888-4888-8888-888888888888',canEdit:true,canAdmin:false})}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({tenant:'11111111-1111-4111-8111-111111111111',id:'22222222-2222-4222-8222-222222222222'}),useRouter:()=>({push:mocks.push})}));
const client={request:async()=>userProfile('88888888-8888-4888-8888-888888888888'),origin:'https://semantic-route.example',semanticTargets:mocks.semanticTargets,materializeSemanticShownVersion:mocks.materializeSemanticShownVersion};
const auth={client:()=>client};
vi.mock('./auth-context',()=>({useAuth:()=>auth}));
vi.mock('./use-editor-capabilities',()=>({useEditorCapabilities:()=>({capabilities:undefined})}));
import SemanticTargets from '../screens/semantic/[id]';
const tenant='11111111-1111-4111-8111-111111111111',intent='22222222-2222-4222-8222-222222222222',target='33333333-3333-4333-8333-333333333333',project='44444444-4444-4444-8444-444444444444',source='55555555-5555-4555-8555-555555555555',accepted='66666666-6666-4666-8666-666666666666',asset='77777777-7777-4777-8777-777777777777';
afterEach(()=>{cleanup();vi.resetAllMocks()});
it('opens exact materialized version in editor with target project, asset and tenant through the real screen',async()=>{
 mocks.semanticTargets.mockResolvedValue({items:[{id:target,revision:'2',document:null,data:{resource_type:'semantic_targets',id:target,tenant_id:tenant,project_id:project,created_at:'2026-10-16T12:00:00Z',updated_at:'2026-10-16T12:00:00Z',revision:'2',intent_id:intent,asset_version_id:source,state:'applied',child_run_id:null,batch_id:null,candidate_artifact_id:null,review_id:null,override_id:null}}],next_cursor:null});
 mocks.materializeSemanticShownVersion.mockResolvedValue({asset_version_id:accepted,asset_id:asset});
 render(<SemanticTargets/>);await clickWhenEnabled(await screen.findByRole('button',{name:'Open edited photo'}));
 await waitFor(()=>expect(mocks.push).toHaveBeenCalledWith({pathname:'/editor/[version]',params:{version:accepted,asset,project,tenant}}));
 expect(mocks.materializeSemanticShownVersion.mock.calls[0].slice(0,2)).toEqual([tenant,target]);expect(mocks.push).toHaveBeenCalledOnce();
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
