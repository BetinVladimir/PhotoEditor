// @vitest-environment jsdom
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
const navigation=vi.hoisted(()=>({push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({router:{push:navigation.push}}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import {WorkspaceAccessContext} from './workspace-access-context';
import {WorkspaceSettingsNavigation} from './navigation-entry-links';
const tenant='11111111-1111-4111-8111-111111111111',actor='22222222-2222-4222-8222-222222222222';
afterEach(()=>{cleanup();vi.clearAllMocks();});

it('owner sees billing, payment-details, usage, and common settings',()=>{
 render(<WorkspaceAccessContext.Provider value={{tenant,actor,role:'owner',canEdit:true,canAdmin:true}}><WorkspaceSettingsNavigation tenant={tenant}/></WorkspaceAccessContext.Provider>);
 expect(screen.getByText('Billing')).toBeTruthy();
 expect(screen.getByText('Payment methods and invoices')).toBeTruthy();
 expect(screen.getByText('Usage')).toBeTruthy();
 expect(screen.getByText('Members')).toBeTruthy();
 expect(screen.getByText('Privacy')).toBeTruthy();
});

it('owner billing nav pushes correct routes',()=>{
 render(<WorkspaceAccessContext.Provider value={{tenant,actor,role:'owner',canEdit:true,canAdmin:true}}><WorkspaceSettingsNavigation tenant={tenant}/></WorkspaceAccessContext.Provider>);
 fireEvent.click(screen.getByText('Billing'));
 fireEvent.click(screen.getByText('Payment methods and invoices'));
 fireEvent.click(screen.getByText('Usage'));
 expect(navigation.push.mock.calls.map(([t])=>t)).toEqual([
  {pathname:'/settings/billing',params:{tenant}},
  {pathname:'/settings/billing-payment-details',params:{tenant}},
  {pathname:'/settings/usage',params:{tenant}},
 ]);
});

it('admin sees usage but not billing or payment-details',()=>{
 render(<WorkspaceAccessContext.Provider value={{tenant,actor,role:'admin',canEdit:true,canAdmin:true}}><WorkspaceSettingsNavigation tenant={tenant}/></WorkspaceAccessContext.Provider>);
 expect(screen.queryByText('Billing')).toBeNull();
 expect(screen.queryByText('Payment methods and invoices')).toBeNull();
 expect(screen.getByText('Usage')).toBeTruthy();
 expect(screen.getByText('Members')).toBeTruthy();
 expect(screen.getByText('Privacy')).toBeTruthy();
});

it('viewer does not see billing, payment-details, or usage',()=>{
 render(<WorkspaceAccessContext.Provider value={{tenant,actor,role:'viewer',canEdit:false,canAdmin:false}}><WorkspaceSettingsNavigation tenant={tenant}/></WorkspaceAccessContext.Provider>);
 expect(screen.queryByText('Billing')).toBeNull();
 expect(screen.queryByText('Payment methods and invoices')).toBeNull();
 expect(screen.queryByText('Usage')).toBeNull();
 expect(screen.getByText('Members')).toBeTruthy();
 expect(screen.getByText('Privacy')).toBeTruthy();
});

it('editor does not see billing, payment-details, or usage',()=>{
 render(<WorkspaceAccessContext.Provider value={{tenant,actor,role:'editor',canEdit:true,canAdmin:false}}><WorkspaceSettingsNavigation tenant={tenant}/></WorkspaceAccessContext.Provider>);
 expect(screen.queryByText('Billing')).toBeNull();
 expect(screen.queryByText('Payment methods and invoices')).toBeNull();
 expect(screen.queryByText('Usage')).toBeNull();
 expect(screen.getByText('Members')).toBeTruthy();
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
