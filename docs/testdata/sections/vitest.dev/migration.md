---
url: /guide/migration.md
---

# Migrating to Vitest 5.0 {#vitest-5}

[Migrating to Vitest 4.0](https://v4.vitest.dev/guide/migration) | [Migrating to Vitest 3.0](https://v3.vitest.dev/guide/migration)

::: warning Prerequisites
Vitest 5.0 requires Vite >= 6.4.0 and Node.js >= 22.12.0. Before proceeding with any other migration steps, ensure your environment meets these requirements. Running Vitest 5.0 on older versions of Vite or Node.js is not supported and may result in unexpected errors.
:::

## Yarn Users Must Install `vite` Explicitly

`vite` is no longer a direct dependency of `vitest`. It is now a required peer dependency, so the version of Vite used by Vitest is the one installed in your project. npm, pnpm, Bun, and Deno install peer dependencies automatically. Yarn does not, so after the upgrade Vitest cannot resolve `vite` unless it is listed in your `package.json`:

```bash
yarn add -D vite
```

## `clearMocks` is Enabled by Default

[`clearMocks`](/config/clearmocks) now defaults to `true`: Vitest calls [`vi.clearAllMocks()`](/api/vi#vi-clearallmocks) before every test, clearing the recorded history of every mock while leaving implementations intact.

In practice this means a mock no longer carries calls from one test into the next:

```ts
import { expect, test, vi } from 'vitest'

const fn = vi.fn()

test('first', () => {
  fn()
  expect(fn).toHaveBeenCalledTimes(1)
})

test('second', () => {
  fn()
  // v4: the call from "first" was kept, so this was 2 // [!code --]
  expect(fn).toHaveBeenCalledTimes(2) // [!code --]
  // v5: history is cleared before each test, so only this test's call counts // [!code ++]
  expect(fn).toHaveBeenCalledTimes(1) // [!code ++]
})
```

Tests that record calls outside of the test body (for example in a setup file, at the top level of a module, or in a `beforeAll` hook) are the most affected, because that history is cleared before the test that asserts on it runs.

To keep the previous behavior, set `clearMocks` back to `false`:

```ts [vitest.config.ts]
import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    clearMocks: false, // [!code ++]
  },
})
```

## `testNamePattern` Matches the `>`-Joined Full Name

[`testNamePattern`](/config/testnamepattern) (the `-t` CLI flag) now matches against the test's full name with the suite chain and test name joined by `' > '`, the same string shown in the reporter output. Previously the segments were joined with a single space, mirroring Jest.

Only patterns that span the boundary between two segments are affected:

```ts
describe('math', () => {
  test('adds', () => {})
})
```
