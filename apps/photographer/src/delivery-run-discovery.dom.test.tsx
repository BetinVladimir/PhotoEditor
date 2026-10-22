// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {OwnerAPI} from './owner-api';
vi.mock('react-native',async()=>import('react-native-web'));
import {DeliveryRunDiscovery} from './delivery-run-discovery';
afterEach(cleanup);
it('shows cancellable initial observation and ignores late response after readonly retry',async()=>{
 let resolve!:(v:unknown)=>void;const api={deliveryPackageForRun:vi.fn().mockReturnValueOnce(new Promise(done=>{resolve=done})).mockResolvedValue({id:'new-package'})};const onOpen=vi.fn();render(<DeliveryRunDiscovery api={api as unknown as OwnerAPI} run="run" onOpen={onOpen}/>);fireEvent.click(screen.getByText('Stop loading'));fireEvent.click(screen.getByText('Retry loading'));await screen.findByText('Delivery package');await act(async()=>resolve({id:'old-package'}));fireEvent.click(screen.getByText('Delivery package'));expect(onOpen).toHaveBeenCalledWith({id:'new-package'});expect(api.deliveryPackageForRun).toHaveBeenCalledTimes(2);
});
