Routes map a method and a path to a handler. Every route is matched in the order it was added, so a catch-all route belongs at the end of the list, after the routes it should not hide.

Use `app.get` to add a route. The handler receives the context, e.g. the request and its params. Return a response from it:

```ts
app.get('/', (c) => c.text('Hello'))
```

```js
app.get('/', (c) => c.text('Hello'))
```

Add a second route with a path parameter.
```ts
app.get('/user/:id', (c) => c.text(c.req.param('id')))
```

A route added after a catch-all never runs, because the catch-all matches first. Keep the catch-all route last, and register middleware before the routes it should wrap.

Run the app with your package manager. Pick one:

npm

```sh
npm run dev
```

bun

```sh
bun run dev
```
