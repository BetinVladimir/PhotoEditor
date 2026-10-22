// @vitest-environment jsdom
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const navigation=vi.hoisted(()=>({push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{push:navigation.push}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import {WorkspaceAccessContext} from './workspace-access-context';
import {ProfileNavigation,WorkspaceSettingsNavigation,SessionAnalysisNavigation} from './navigation-entry-links';
import {NavigationReturnLinks} from './navigation-return-links';
const tenant='11111111-1111-4111-8111-111111111111',id='22222222-2222-4222-8222-222222222222',project='33333333-3333-4333-8333-333333333333';
afterEach(()=>{cleanup();vi.clearAllMocks();});
it('makes all workspace settings and session analysis reachable to Viewer without producer authority',()=>{
 render(<WorkspaceAccessContext.Provider value={{tenant,actor:id,role:'viewer',canEdit:false,canAdmin:false}}><WorkspaceSettingsNavigation tenant={tenant}/><SessionAnalysisNavigation tenant={tenant} id={id} project={project}/></WorkspaceAccessContext.Provider>);
 for(const label of ['Members','Quotas and capabilities','Retention','Session analysis and scenes'])fireEvent.click(screen.getByText(label));
 expect(navigation.push.mock.calls.map(([target])=>target)).toEqual([
 {pathname:'/settings/members',params:{tenant}},{pathname:'/settings/quotas',params:{tenant}},{pathname:'/settings/retention',params:{tenant}},{pathname:'/sessions/[id]',params:{tenant,id,project}},
 ]);
});
it('withholds workspace entries without exact current authority while global profile carries no tenant',()=>{
 render(<><WorkspaceSettingsNavigation tenant={tenant}/><SessionAnalysisNavigation tenant={tenant} id={id} project={project}/><ProfileNavigation/></>);
 expect(screen.queryByText('Members')).toBeNull();expect(screen.queryByText('Session analysis and scenes')).toBeNull();fireEvent.click(screen.getByText('Profile'));expect(navigation.push).toHaveBeenCalledWith({pathname:'/settings/profile'});
});
it('provides direct-entry escape without a back stack and refuses withdrawn authority',()=>{
 let current=true;render(<NavigationReturnLinks tenant={tenant} current={()=>current}/>);
 fireEvent.click(screen.getByText('Workspace projects'));expect(navigation.push).toHaveBeenCalledWith({pathname:'/projects',params:{tenant}});
 current=false;fireEvent.click(screen.getByText('Workspaces'));expect(navigation.push).toHaveBeenCalledOnce();
});
it('global direct-entry return never forwards a tenant parameter',()=>{
 render(<NavigationReturnLinks/>);expect(screen.queryByText('Workspace projects')).toBeNull();fireEvent.click(screen.getByText('Workspaces'));expect(navigation.push).toHaveBeenCalledWith('/workspaces');
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
