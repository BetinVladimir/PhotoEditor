// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {DeliveryRunLink} from './delivery-run-link';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(cleanup);

it('opens a package only after the run/workflow binding and scoped package read',async()=>{
 const get=vi.fn(async()=>({run_id:id(1),workflow_id:id(2)}));
 const deliveryPackageForWorkflow=vi.fn(async()=>({id:id(4)}));
 const onOpen=vi.fn();
 render(<DeliveryRunLink api={{get,deliveryPackageForWorkflow} as unknown as OwnerAPI} run={id(1)} workflow={id(2)} project={id(3)} onOpen={onOpen}/>);
 fireEvent.click(await screen.findByText('Delivery package'));
 expect(onOpen).toHaveBeenCalledWith(id(4));
 expect(deliveryPackageForWorkflow).toHaveBeenCalledWith(id(3),id(2),expect.any(AbortSignal));
});
it('refuses a foreign run before requesting a package',async()=>{
 const get=vi.fn(async()=>({run_id:id(8),workflow_id:id(2)}));
 const deliveryPackageForWorkflow=vi.fn();
 render(<DeliveryRunLink api={{get,deliveryPackageForWorkflow} as unknown as OwnerAPI} run={id(1)} workflow={id(2)} project={id(3)} onOpen={vi.fn()}/>);
 await waitFor(()=>expect(screen.getByText('Refresh')).toBeTruthy());
 expect(deliveryPackageForWorkflow).not.toHaveBeenCalled();
 expect(screen.queryByText('Delivery package')).toBeNull();
});
