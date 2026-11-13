# Programmatic Navigation

## router.push(location, onComplete?, onAbort?)

To navigate to a different URL, use `router.push`. This method pushes a new entry into the history stack.

The argument can be a string path, or a location descriptor object. Examples:

```js
router.push('home')
router.push({ path: 'home' })
```

Optionally provide `onComplete` and `onAbort` callbacks to `router.push` or `router.replace` as the 2nd and 3rd arguments. These callbacks are called when the navigation completed, or was aborted, respectively.

```js
router.push('/users/1', () => console.log('done'), (err) => console.log('aborted', err))
```
