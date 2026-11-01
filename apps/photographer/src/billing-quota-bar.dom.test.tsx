// @vitest-environment jsdom
import {cleanup,render,screen} from '@testing-library/react';
import {afterEach,expect,it} from 'vitest';
import {vi} from 'vitest';
vi.mock('react-native',async()=>import('react-native-web'));
import {QuotaBar} from './billing-quota-bar';

afterEach(()=>cleanup());

it('renders progress bar with accessible label', () => {
  render(<QuotaBar used={50n} granted={100n} label="Storage usage" overflowLabel="Over limit"/>);
  expect(screen.getByRole('progressbar')).toBeTruthy();
  expect(screen.getByLabelText('Storage usage')).toBeTruthy();
});

it('does not show overflow label when within limit (criterion C)', () => {
  render(<QuotaBar used={80n} granted={100n} label="Storage" overflowLabel="Exceeds limit"/>);
  expect(screen.queryByText('Exceeds limit')).toBeNull();
});

it('shows overflow label when used exceeds granted (criterion C)', () => {
  render(<QuotaBar used={120n} granted={100n} label="Storage" overflowLabel="Quota exceeded"/>);
  expect(screen.getByText('Quota exceeded')).toBeTruthy();
});

it('does not show overflow when granted is zero', () => {
  render(<QuotaBar used={0n} granted={0n} label="Storage" overflowLabel="Should not appear"/>);
  expect(screen.queryByText('Should not appear')).toBeNull();
});

it('overflow label has alert role for screen reader announcement', () => {
  render(<QuotaBar used={110n} granted={100n} label="Storage" overflowLabel="Over quota"/>);
  expect(screen.getByRole('alert')).toBeTruthy();
});
