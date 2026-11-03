# Migrating to Vue Router 4

## router.push callbacks are removed

Previously, `router.push()` accepted `onComplete` and `onAbort` callbacks as second and third arguments. With the Promise based API, these callbacks have been removed.

```js
router.push('/users/1').then(() => console.log('done'))
```
