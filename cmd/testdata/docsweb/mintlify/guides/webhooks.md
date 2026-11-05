# Webhooks

Acme sends webhook events to your endpoint whenever a payment changes state.

## Register an endpoint

Add your HTTPS endpoint in the dashboard and choose which events it receives.

## Verify webhook signatures

Each webhook request carries an Acme-Signature header. Compute an HMAC-SHA256 of the raw request body with your endpoint secret and compare it to the header before you trust the event.

```js
const valid = acme.webhooks.verify(rawBody, req.headers["acme-signature"], secret);
```

## Retries

Failed deliveries are retried with exponential backoff for three days.
