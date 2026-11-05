---
url: https://vite.dev/config/build-options
title: Build Options ​
words: 2314
---
# Build Options ​

Unless noted, the options in this section are only applied to build.

## build.target ​

- **Type:** `string | string[]`
- **Default:** `'baseline-widely-available'`
- **Related:** [Browser Compatibility](https://vite.dev/guide/build#browser-compatibility)

Browser compatibility target for the final bundle. The default value is a Vite special value, `'baseline-widely-available'`, which targets the minimum browser versions compatible with [Baseline](https://web-platform-dx.github.io/baseline/) Widely Available as of a date fixed for each major release ([2026-01-01 for this major](https://web-platform-dx.github.io/supported-browsers/?widelyAvailableOnDate=2026-01-01)). Specifically, it is `['chrome111', 'edge111', 'firefox114', 'safari16.4', 'ios16.4']`.

Another special value is `'esnext'` - which assumes native dynamic imports support and will only perform minimal transpiling.

The transform is performed with Oxc Transformer and the value should be a valid [Oxc Transformer target option](https://oxc.rs/docs/guide/usage/transformer/lowering#target). Custom targets can either be an ES version (e.g. `es2015`), a browser with version (e.g. `chrome58`), or an array of multiple target strings.

Note the build will output a warning if the code contains features that cannot be safely transpiled by Oxc. See [Oxc docs](https://oxc.rs/docs/guide/usage/transformer/lowering#warnings) for more details.

## build.modulePreload ​

- **Type:** `boolean | { polyfill?: boolean, resolveDependencies?: ResolveModulePreloadDependenciesFn }`
- **Default:** `{ polyfill: true }`

By default, a [module preload polyfill](https://guybedford.com/es-module-preloading-integrity#modulepreload-polyfill) is automatically injected. The polyfill is auto injected into the proxy module of each `index.html` entry. If the build is configured to use a non-HTML custom entry via `build.rolldownOptions.input`, then it is necessary to manually import the polyfill in your custom entry:

js

```
import 'vite/modulepreload-polyfill'
```
Note: the polyfill does **not** apply to [Library Mode](https://vite.dev/guide/build#library-mode). If you need to support browsers without native dynamic import, you should probably avoid using it in your library.

The polyfill can be disabled using `{ polyfill: false }`.

The list of chunks to preload for each dynamic import is computed by Vite. By default, an absolute path including the `base` will be used when loading these dependencies. If the `base` is relative (`''` or `'./'`), `import.meta.url` is used at runtime to avoid absolute paths that depend on the final deployed base.

There is experimental support for fine grained control over the dependencies list and their paths using the `resolveDependencies` function. [Give Feedback](https://github.com/vitejs/vite/discussions/13841). It expects a function of type `ResolveModulePreloadDependenciesFn`:

ts

```
type ResolveModulePreloadDependenciesFn = (
  url: string,
  deps: string[],
  context: {
    hostId: string
    hostType: 'html' | 'js'
  },
) => string[]
```
The `resolveDependencies` function will be called for each dynamic import with a list of the chunks it depends on, and it will also be called for each chunk imported in entry HTML files. A new dependencies array can be returned with these filtered or more dependencies injected, and their paths modified. The `deps` paths are relative to the `build.outDir`. The return value should be a relative path to the `build.outDir`.

js

```
modulePreload




: {
  
resolveDependencies




: (
filename




, 
deps




, { 
hostId




, 
hostType




 }) => {
    return 
deps




.
filter




(condition)
  },
},
```
The resolved dependency paths can be further modified using [`experimental.renderBuiltUrl`](https://vite.dev/guide/build#advanced-base-options).

## build.polyfillModulePreload ​

- **Type:** `boolean`
- **Default:** `true`
- **Deprecated** use `build.modulePreload.polyfill` instead

Whether to automatically inject a [module preload polyfill](https://guybedford.com/es-module-preloading-integrity#modulepreload-polyfill).

## build.outDir ​

- **Type:** `string`
- **Default:** `dist`

Specify the output directory (relative to [project root](https://vite.dev/guide/#index-html-and-project-root)).

## build.assetsDir ​

- **Type:** `string`
- **Default:** `assets`

Specify the directory to nest generated assets under (relative to `build.outDir`). This is not used in [Library Mode](https://vite.dev/guide/build#library-mode).

## build.assetsInlineLimit ​

- **Type:** `number` | `((filePath: string, content: Buffer) => boolean | undefined)`
- **Default:** `4096` (4 KiB)

Imported or referenced assets that are smaller than this threshold will be inlined as base64 URLs to avoid extra http requests. Set to `0` to disable inlining altogether.

If a callback is passed, a boolean can be returned to opt-in or opt-out. If nothing is returned the default logic applies.

Git LFS placeholders are automatically excluded from inlining because they do not contain the content of the file they represent.

Note

If you specify `build.lib`, `build.assetsInlineLimit` will be ignored and assets will always be inlined, regardless of file size or being a Git LFS placeholder.

## build.cssCodeSplit ​

- **Type:** `boolean`
- **Default:** `true`

Enable/disable CSS code splitting. When enabled, CSS imported in async JS chunks will be preserved as chunks and fetched together when the chunk is fetched.

If disabled, all CSS in the entire project will be extracted into a single CSS file.
## build.lib ​

- **Type:** `{ entry?: string | string[] | { [entryAlias: string]: string }, name?: string, formats?: ('es' | 'cjs' | 'umd' | 'iife')[], fileName?: string | ((format: ModuleFormat, entryName: string) => string), cssFileName?: string }`
- **Related:** [Library Mode](https://vite.dev/guide/build#library-mode)

Build as a library. `entry` defaults to the top-level [`input`](https://vite.dev/config/shared-options#input) option, and one of them is required since the library cannot use HTML as entry. `name` is the exposed global variable and is required when `formats` includes `'umd'` or `'iife'`. Default `formats` are `['es', 'umd']`, or `['es', 'cjs']`, if multiple entries are used.

`fileName` is the name of the package file output, which defaults to the `"name"` in `package.json`. It can also be defined as a function taking the `format` and `entryName` as arguments, and returning the file name.

If your package imports CSS, `cssFileName` can be used to specify the name of the CSS file output. It defaults to the same value as `fileName` if it's set a string, otherwise it also falls back to the `"name"` in `package.json`.

vite.config.js

js

```
import { 
defineConfig




 } from 'vite'

export default 
defineConfig




({
  
build




: {
    
lib




: {
      
entry




: ['src/main.js'],
      
fileName




: (
format




, 
entryName




) => `my-lib-${
entryName




}.${
format




}.js`,
      
cssFileName




: 'my-lib-style',
    },
  },
})
```
