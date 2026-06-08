# Frontend Vue Vite

## Keep PrismGo Serving Boundaries Clear

PrismGo projects commonly build Vue/Vite assets into a backend-served public directory. Keep the Vite output path and backend static serving contract aligned.

```ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: '../public',
    emptyOutDir: true,
  },
})
```

## Use API Clients Instead Of Inline Fetch Scattering

Centralize base URLs, auth headers, and error handling in API modules or composables.

Incorrect:

```ts
await fetch('/api/v1/users', { method: 'POST', body: JSON.stringify(form) })
```

Correct:

```ts
export function createUser(payload: CreateUserPayload) {
  return http.post('/users', payload)
}
```

## Keep Permission And Route Metadata Declarative

When a PrismGo application exposes authorization data, keep frontend route metadata declarative and use shared composables or directives for checks.

```ts
{
  path: '/users',
  component: () => import('@/views/users/ListView.vue'),
  meta: { permission: 'users.list' },
}
```

## Verify Built Assets When Changing Integration

Changes to Vite config, backend static routes, base paths, or SPA fallback should be verified with the project build command and a browser smoke test.

```bash
npm run build
```
