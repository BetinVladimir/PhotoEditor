// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {APIError} from '@photoeditor/api-client';
const mocks=vi.hoisted(()=>{const client={request:vi.fn()};return {params:{tenant:'',project:''},push:vi.fn(),deliveryPackages:vi.fn(),client,auth:{client:()=>client}};});
vi.mock('react-native',async()=>import('react-native-web'));
const lifecycle=vi.hoisted(()=>({active:true,generation:0}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>lifecycle}));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>mocks.params,router:{push:mocks.push}}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class {deliveryPackages=mocks.deliveryPackages;}}));
import DeliveryListRoute from '../screens/deliveries/index';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const item=(n:number)=>({id:id(n),project_id:id(3),workflow_id:id(n+100),name:`Delivery ${n}`,state:'ready',revision:'1',created_at:'2026-10-13T12:00:00Z'});
beforeEach(()=>{mocks.params={tenant:id(2),project:id(3)};mocks.auth={client:()=>mocks.client};mocks.push.mockReset();mocks.deliveryPackages.mockReset();});
afterEach(()=>{cleanup();lifecycle.active=true;lifecycle.generation=0;});
it('loads every page and opens its project-scoped package',async()=>{
 mocks.deliveryPackages.mockResolvedValueOnce({items:[item(1)],next_cursor:'next'}).mockResolvedValueOnce({items:[item(2)],next_cursor:null});
 render(<DeliveryListRoute/>);await screen.findByText('Delivery 1');
 fireEvent.click(screen.getByText('Load more packages'));
 await screen.findByText('Delivery 2');
 expect(mocks.deliveryPackages.mock.calls[1][0]).toBe(id(3));expect(mocks.deliveryPackages.mock.calls[1][1]).toBe('next');
 expect(mocks.deliveryPackages.mock.calls[1][4]).toEqual(item(1));
 fireEvent.click(screen.getByText('Delivery 2'));
 expect(mocks.push).toHaveBeenCalledWith({pathname:'/deliveries/[id]',params:{tenant:id(2),project:id(3),id:id(2)}});
});
it('rejects repeated cursor instead of silently truncating the list',async()=>{
 mocks.deliveryPackages.mockResolvedValueOnce({items:[item(1)],next_cursor:'next'}).mockResolvedValueOnce({items:[item(2)],next_cursor:'next'});
 render(<DeliveryListRoute/>);await screen.findByText('Delivery 1');fireEvent.click(screen.getByText('Load more packages'));
 await waitFor(()=>expect(screen.getByRole('alert').textContent).toContain('Could not load delivery packages'));
 expect(screen.queryByText('Delivery 2')).toBeNull();
});
it('drops previously loaded package metadata when project access is revoked',async()=>{
 mocks.deliveryPackages.mockResolvedValueOnce({items:[item(1)],next_cursor:'next'}).mockRejectedValueOnce(new APIError(403,'FORBIDDEN'));
 render(<DeliveryListRoute/>);await screen.findByText('Delivery 1');
 fireEvent.click(screen.getByText('Load more packages'));
 await waitFor(()=>expect(screen.getByRole('alert').textContent).toContain('Could not load delivery packages'));
 expect(screen.queryByText('Delivery 1')).toBeNull();
 expect(screen.queryByText('Load more packages')).toBeNull();
});
it('drops old user package metadata before loading the same project with a new client',async()=>{
 mocks.deliveryPackages.mockResolvedValueOnce({items:[item(1)],next_cursor:null}).mockResolvedValueOnce({items:[item(2)],next_cursor:null});
 const view=render(<DeliveryListRoute/>);
 await screen.findByText('Delivery 1');
 const anotherClient={request:vi.fn()};mocks.auth={client:()=>anotherClient};
 view.rerender(<DeliveryListRoute/>);
 expect(screen.queryByText('Delivery 1')).toBeNull();
 await screen.findByText('Delivery 2');
 expect(mocks.deliveryPackages).toHaveBeenCalledTimes(2);
});

it('clears package metadata in background and reloads on foreground return',async()=>{
 mocks.deliveryPackages.mockResolvedValueOnce({items:[item(1)],next_cursor:null}).mockResolvedValueOnce({items:[item(2)],next_cursor:null});
 const view=render(<DeliveryListRoute/>);await screen.findByText('Delivery 1');
 lifecycle.active=false;view.rerender(<DeliveryListRoute/>);
 expect(screen.queryByText('Delivery 1')).toBeNull();expect(mocks.push).not.toHaveBeenCalled();
 lifecycle.active=true;lifecycle.generation++;view.rerender(<DeliveryListRoute/>);
 await screen.findByText('Delivery 2');expect(mocks.deliveryPackages).toHaveBeenCalledTimes(2);
});

it('cancels a continuation and retries exactly that page without losing earlier packages',async()=>{
 let finish!:(value:unknown)=>void;
 mocks.deliveryPackages.mockResolvedValueOnce({items:[item(1)],next_cursor:'next'}).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValueOnce({items:[item(2)],next_cursor:null});
 render(<DeliveryListRoute/>);await screen.findByText('Delivery 1');fireEvent.click(screen.getByText('Load more packages'));
 fireEvent.click(await screen.findByText('Stop loading'));await act(async()=>finish({items:[item(9)],next_cursor:null}));
 expect(screen.queryByText('Delivery 9')).toBeNull();expect(screen.getByText('Delivery 1')).toBeTruthy();
 fireEvent.click(screen.getByText('Retry loading'));await screen.findByText('Delivery 2');
 expect(mocks.deliveryPackages.mock.calls[2][1]).toBe('next');
});

vi.mock('./locale-context',async()=>{const {default:en}=await import('../../../locales/en/common.json');const {withUILocaleDouble}=await import('./test/locale-double');return {useLocale:()=>withUILocaleDouble({locale:'en',setLocale:()=>{},t:(key:keyof typeof en)=>en[key]})};});
