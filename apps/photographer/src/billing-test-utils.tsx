// Test-only render helper for billing dom tests.
//
// Usage: import {renderBillingScreen, BILLING_TEST_TENANT} from './billing-test-utils'.
// vi.mock calls for locale-context, owner-api and localized-router must remain
// at module level in each calling test file (vitest constraint).
import {render, type RenderResult} from '@testing-library/react';
import {type ReactElement} from 'react';
import {PhotographerPaperProvider} from './paper-provider';

/** Fixed UUIDs used across billing dom tests to keep assertions stable. */
export const BILLING_TEST_TENANT  = '11111111-1111-4111-8111-111111111111';
export const BILLING_TEST_TENANT2 = '22222222-2222-4222-8222-222222222222';
export const BILLING_TEST_USER    = '33333333-3333-4333-8333-333333333333';

/** Wraps the given element in Paper providers required for billing screen rendering. */
export function renderBillingScreen(ui: ReactElement): RenderResult {
  return render(<PhotographerPaperProvider>{ui}</PhotographerPaperProvider>);
}
