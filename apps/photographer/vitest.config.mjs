import{defineConfig}from"vitest/config";import{fileURLToPath}from"node:url";
export default defineConfig({resolve:{alias:{"expo-secure-store":fileURLToPath(new URL("./src/test/secure-store.ts",import.meta.url))}}});
