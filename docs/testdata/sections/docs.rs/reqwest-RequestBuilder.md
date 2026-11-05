---
url: https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html
title: RequestBuilder in reqwest
words: 1685
---
# Struct RequestBuilder

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#39-42)

```
pub struct RequestBuilder { /* private fields */ }
```
Available on **not (WebAssembly and (`target_os=unknown` or bare-metal))**.

Expand description

A builder to construct the properties of a `Request`.

To construct a `RequestBuilder`, refer to the `Client` documentation.

## Implementations

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#171-557)[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-RequestBuilder)

### impl [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#189-194)

#### pub fn from\_parts(client: [Client](https://docs.rs/reqwest/latest/reqwest/struct.Client.html "struct reqwest::Client"), request: [Request](https://docs.rs/reqwest/latest/reqwest/struct.Request.html "struct reqwest::Request")) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Assemble a builder starting from an existing `Client` and a `Request`.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#197-205)

#### pub fn header&lt;K, V&gt;(self, key: K, value: V) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder") where [HeaderName](https://docs.rs/reqwest/latest/reqwest/header/struct.HeaderName.html "struct reqwest::header::HeaderName"): [TryFrom](https://doc.rust-lang.org/nightly/core/convert/trait.TryFrom.html "trait core::convert::TryFrom")&lt;K&gt;, &lt;[HeaderName](https://docs.rs/reqwest/latest/reqwest/header/struct.HeaderName.html "struct reqwest::header::HeaderName") as [TryFrom](https://doc.rust-lang.org/nightly/core/convert/trait.TryFrom.html "trait core::convert::TryFrom")&lt;K&gt;&gt;::[Error](https://doc.rust-lang.org/nightly/core/convert/trait.TryFrom.html#associatedtype.Error "type core::convert::TryFrom::Error"): [Into](https://doc.rust-lang.org/nightly/core/convert/trait.Into.html "trait core::convert::Into")&lt;[Error](https://docs.rs/http/1.5.0/x86_64-unknown-linux-gnu/http/error/struct.Error.html "struct http::error::Error")&gt;, [HeaderValue](https://docs.rs/reqwest/latest/reqwest/header/struct.HeaderValue.html "struct reqwest::header::HeaderValue"): [TryFrom](https://doc.rust-lang.org/nightly/core/convert/trait.TryFrom.html "trait core::convert::TryFrom")&lt;V&gt;, &lt;[HeaderValue](https://docs.rs/reqwest/latest/reqwest/header/struct.HeaderValue.html "struct reqwest::header::HeaderValue") as [TryFrom](https://doc.rust-lang.org/nightly/core/convert/trait.TryFrom.html "trait core::convert::TryFrom")&lt;V&gt;&gt;::[Error](https://doc.rust-lang.org/nightly/core/convert/trait.TryFrom.html#associatedtype.Error "type core::convert::TryFrom::Error"): [Into](https://doc.rust-lang.org/nightly/core/convert/trait.Into.html "trait core::convert::Into")&lt;[Error](https://docs.rs/http/1.5.0/x86_64-unknown-linux-gnu/http/error/struct.Error.html "struct http::error::Error")&gt;,

Add a `Header` to this Request.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#242-247)

#### pub fn headers(self, headers: [HeaderMap](https://docs.rs/reqwest/latest/reqwest/header/struct.HeaderMap.html "struct reqwest::header::HeaderMap")) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Add a set of Headers to the existing ones on this Request.

The headers will be merged in to any already set.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#263-270)

#### pub fn basic\_auth&lt;U, P&gt;( self, username: U, password: [Option](https://doc.rust-lang.org/nightly/core/option/enum.Option.html "enum core::option::Option")&lt;P&gt;, ) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder") where U: [Display](https://doc.rust-lang.org/nightly/core/fmt/trait.Display.html "trait core::fmt::Display"), P: [Display](https://doc.rust-lang.org/nightly/core/fmt/trait.Display.html "trait core::fmt::Display"),

Enable HTTP basic authentication.

```

let client = reqwest::Client::new();
let resp = client.delete("http://httpbin.org/delete")
    .basic_auth("admin", Some("good password"))
    .send()
    .await?;
```
[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#273-279)

#### pub fn bearer\_auth&lt;T&gt;(self, token: T) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder") where T: [Display](https://doc.rust-lang.org/nightly/core/fmt/trait.Display.html "trait core::fmt::Display"),

Enable HTTP bearer authentication.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#282-287)

#### pub fn body&lt;T: [Into](https://doc.rust-lang.org/nightly/core/convert/trait.Into.html "trait core::convert::Into")&lt;[Body](https://docs.rs/reqwest/latest/reqwest/struct.Body.html "struct reqwest::Body")&gt;&gt;(self, body: T) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Set the request body.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#294-299)

#### pub fn timeout(self, timeout: [Duration](https://doc.rust-lang.org/nightly/core/time/struct.Duration.html "struct core::time::Duration")) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Enables a request timeout.

The timeout is applied from when the request starts connecting until the response body has finished. It affects only this request and overrides the timeout configured using `ClientBuilder::timeout()`.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#325-340)

#### pub fn multipart(self, multipart: [Form](https://docs.rs/reqwest/latest/reqwest/multipart/struct.Form.html "struct reqwest::multipart::Form")) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Available on **crate feature `multipart`** only.

Sends a multipart/form-data body.

```

let client = reqwest::Client::new();
let form = reqwest::multipart::Form::new()
    .text("key3", "value3")
    .text("key4", "value4");


let response = client.post("your url")
    .multipart(form)
    .send()
    .await?;
```
In additional the request’s body, the Content-Type and Content-Length fields are appropriately set.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#366-386)

#### pub fn query&lt;T: [Serialize](https://docs.rs/serde_core/1.0.229/x86_64-unknown-linux-gnu/serde_core/ser/trait.Serialize.html "trait serde_core::ser::Serialize") + ?[Sized](https://doc.rust-lang.org/nightly/core/marker/trait.Sized.html "trait core::marker::Sized")&gt;(self, query: [&T](https://doc.rust-lang.org/nightly/std/primitive.reference.html)) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Available on **crate feature `query`** only.

Modify the query string of the URL.

Modifies the URL of this request, adding the parameters provided. This method appends and does not overwrite. This means that it can be called multiple times and that existing query parameters are not overwritten if the same key is used. The key will simply show up twice in the query string. Calling `.query(&[("foo", "a"), ("foo", "b")])` gives `"foo=a&foo=b"`.

##### Note

This method does not support serializing a single key-value pair. Instead of using `.query(("key", "val"))`, use a sequence, such as `.query(&[("key", "val")])`. It’s also possible to serialize structs and maps into a key-value pair.

##### Optional

This requires the optional `query` feature to be enabled.

##### Errors

This method will fail if the object you provide cannot be serialized into a query string.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#389-394)

#### pub fn version(self, version: [Version](https://docs.rs/reqwest/latest/reqwest/struct.Version.html "struct reqwest::Version")) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Set HTTP version

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#429-448)

#### pub fn form&lt;T: [Serialize](https://docs.rs/serde_core/1.0.229/x86_64-unknown-linux-gnu/serde_core/ser/trait.Serialize.html "trait serde_core::ser::Serialize") + ?[Sized](https://doc.rust-lang.org/nightly/core/marker/trait.Sized.html "trait core::marker::Sized")&gt;(self, form: [&T](https://doc.rust-lang.org/nightly/std/primitive.reference.html)) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Available on **crate feature `form`** only.

Send a form body.

Sets the body to the url encoded serialization of the passed value, and also sets the `Content-Type: application/x-www-form-urlencoded` header.

```
let mut params = HashMap::new();
params.insert("lang", "rust");

let client = reqwest::Client::new();
let res = client.post("http://httpbin.org")
    .form(&params)
    .send()
    .await?;
```
##### Optional

This requires the optional `form` feature to be enabled.

##### Errors

This method fails if the passed value cannot be serialized into url encoded format

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#466-483)

#### pub fn json&lt;T: [Serialize](https://docs.rs/serde_core/1.0.229/x86_64-unknown-linux-gnu/serde_core/ser/trait.Serialize.html "trait serde_core::ser::Serialize") + ?[Sized](https://doc.rust-lang.org/nightly/core/marker/trait.Sized.html "trait core::marker::Sized")&gt;(self, json: [&T](https://doc.rust-lang.org/nightly/std/primitive.reference.html)) -&gt; [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

Available on **crate feature `json`** only.

Send a JSON body.

Serializes the value to JSON and sets resulting bytes as the body.

Sets Content-Type header to application/json unless the header is already set.

##### Optional

This requires the optional `json` feature enabled.

##### Errors

Serialization can fail if `T`’s implementation of `Serialize` decides to fail, or if `T` contains a map with non-string keys.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#487-489)

#### pub fn build(self) -&gt; [Result](https://docs.rs/reqwest/latest/reqwest/type.Result.html "type reqwest::Result")&lt;[Request](https://docs.rs/reqwest/latest/reqwest/struct.Request.html "struct reqwest::Request")&gt;

Build a `Request`, which can be inspected, modified and executed with `Client::execute()`.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#496-498)

#### pub fn build\_split(self) -&gt; ([Client](https://docs.rs/reqwest/latest/reqwest/struct.Client.html "struct reqwest::Client"), [Result](https://docs.rs/reqwest/latest/reqwest/type.Result.html "type reqwest::Result")&lt;[Request](https://docs.rs/reqwest/latest/reqwest/struct.Request.html "struct reqwest::Request")&gt;)

Build a `Request`, which can be inspected, modified and executed with `Client::execute()`.

This is similar to [`RequestBuilder::build()`](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#method.build "method reqwest::RequestBuilder::build"), but also returns the embedded `Client`.

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#521-526)

## Trait Implementations

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#565-573)[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-Debug-for-RequestBuilder)

### impl [Debug](https://doc.rust-lang.org/nightly/core/fmt/trait.Debug.html "trait core::fmt::Debug") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[Source](https://docs.rs/reqwest/latest/src/reqwest/async_impl/request.rs.html#566-572)[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#method.fmt)

#### fn [fmt](https://doc.rust-lang.org/nightly/core/fmt/trait.Debug.html#tymethod.fmt)(&self, f: &mut [Formatter](https://doc.rust-lang.org/nightly/core/fmt/struct.Formatter.html "struct core::fmt::Formatter")&lt;'\_&gt;) -&gt; [Result](https://doc.rust-lang.org/nightly/core/fmt/type.Result.html "type core::fmt::Result")

Formats the value using the given formatter. [Read more](https://doc.rust-lang.org/nightly/core/fmt/trait.Debug.html#tymethod.fmt)

## Auto Trait Implementations

[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-Freeze-for-RequestBuilder)

### impl \![Freeze](https://doc.rust-lang.org/nightly/core/marker/trait.Freeze.html "trait core::marker::Freeze") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-RefUnwindSafe-for-RequestBuilder)

### impl \![RefUnwindSafe](https://doc.rust-lang.org/nightly/core/panic/unwind_safe/trait.RefUnwindSafe.html "trait core::panic::unwind_safe::RefUnwindSafe") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-UnwindSafe-for-RequestBuilder)

### impl \![UnwindSafe](https://doc.rust-lang.org/nightly/core/panic/unwind_safe/trait.UnwindSafe.html "trait core::panic::unwind_safe::UnwindSafe") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-Send-for-RequestBuilder)

### impl [Send](https://doc.rust-lang.org/nightly/core/marker/trait.Send.html "trait core::marker::Send") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-Sync-for-RequestBuilder)

### impl [Sync](https://doc.rust-lang.org/nightly/core/marker/trait.Sync.html "trait core::marker::Sync") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-Unpin-for-RequestBuilder)

### impl [Unpin](https://doc.rust-lang.org/nightly/core/marker/trait.Unpin.html "trait core::marker::Unpin") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-UnsafeUnpin-for-RequestBuilder)

### impl [UnsafeUnpin](https://doc.rust-lang.org/nightly/core/marker/trait.UnsafeUnpin.html "trait core::marker::UnsafeUnpin") for [RequestBuilder](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html "struct reqwest::RequestBuilder")

## Blanket Implementations

[Source](https://doc.rust-lang.org/nightly/src/core/any.rs.html#141)[§](https://docs.rs/reqwest/latest/reqwest/struct.RequestBuilder.html#impl-Any-for-T)

