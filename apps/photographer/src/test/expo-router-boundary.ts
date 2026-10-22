/** Standalone DOM suites exercise the real locale provider through URL-backed
 * navigation. Expo's native runtime is outside this headless boundary.
 * Individual route tests can override this mock with their own assertions.
 */
import {vi} from 'vitest';
vi.mock('expo-router',async()=> (await import('./locale-router')).localeRouterFixture);
