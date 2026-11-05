---
url: https://serde.rs/field-attrs.html
title: Field attributes
words: 596
---
# Field attributes

- ##### `#[serde(rename = "name")]`

  Serialize and deserialize this field with the given name instead of its Rust name. This is useful for [serializing fields as camelCase](https://serde.rs/attr-rename.html) or serializing fields with names that are reserved Rust keywords.

  Allows specifying independent names for serialization vs deserialization:

  - `#[serde(rename(serialize = "ser_name"))]`
  - `#[serde(rename(deserialize = "de_name"))]`
  - `#[serde(rename(serialize = "ser_name", deserialize = "de_name"))]`

<!--THE END-->

- ##### `#[serde(alias = "name")]`

  Deserialize this field from the given name *or* from its Rust name. May be repeated to specify multiple possible names for the same field.
- ##### `#[serde(default)]`

  If the value is not present when deserializing, use the `Default::default()`.
- ##### `#[serde(default = "path")]`

  If the value is not present when deserializing, call a function to get a default value. The given function must be callable as `fn() -> T`. For example `default = "empty_value"` would invoke `empty_value()` and `default = "SomeTrait::some_default"` would invoke `SomeTrait::some_default()`.
- ##### `#[serde(flatten)]`

  Flatten the contents of this field into the container it is defined in.

  This removes one level of structure between the serialized representation and the Rust data structure representation. It can be used for factoring common keys into a shared structure, or for capturing remaining fields into a map with arbitrary string keys. The [struct flattening](https://serde.rs/attr-flatten.html) page provides some examples.

*Note:* this attribute is not supported in combination with structs that use [`deny_unknown_fields`](https://serde.rs/container-attrs.html#deny_unknown_fields). Neither the outer nor inner flattened struct should use that attribute.

- ##### `#[serde(skip)]`

  Skip this field: do not serialize or deserialize it.

  When deserializing, Serde will use `Default::default()` or the function given by `default = "..."` to get a default value for this field.
- ##### `#[serde(skip_serializing)]`

  Skip this field when serializing, but not when deserializing.
- ##### `#[serde(skip_deserializing)]`

  Skip this field when deserializing, but not when serializing.

  When deserializing, Serde will use `Default::default()` or the function given by `default = "..."` to get a default value for this field.
- ##### `#[serde(skip_serializing_if = "path")]`

  Call a function to determine whether to skip serializing this field. The given function must be callable as `fn(&T) -> bool`, although it may be generic over `T`. For example `skip_serializing_if = "Option::is_none"` would skip an Option that is None.
- ##### `#[serde(serialize_with = "path")]`

  Serialize this field using a function that is different from its implementation of `Serialize`. The given function must be callable as `fn<S>(&T, S) -> Result<S::Ok, S::Error> where S: Serializer`, although it may also be generic over `T`. Fields used with `serialize_with` are not required to implement `Serialize`.
- ##### `#[serde(deserialize_with = "path")]`

  Deserialize this field using a function that is different from its implementation of `Deserialize`. The given function must be callable as `fn<'de, D>(D) -> Result<T, D::Error> where D: Deserializer<'de>`, although it may also be generic over `T`. Fields used with `deserialize_with` are not required to implement `Deserialize`.
- ##### `#[serde(with = "module")]`

  Combination of `serialize_with` and `deserialize_with`. Serde will use `$module::serialize` as the `serialize_with` function and `$module::deserialize` as the `deserialize_with` function.
- ##### `#[serde(borrow)]` and `#[serde(borrow = "'a + 'b + ...")]`

  Borrow data for this field from the deserializer by using zero-copy deserialization. See [this example](https://serde.rs/lifetimes.html#borrowing-data-in-a-derived-impl).

<!--THE END-->

- ##### `#[serde(bound = "T: MyTrait")]`

  Where-clause for the `Serialize` and `Deserialize` impls. This replaces any trait bounds inferred by Serde for the current field.

  Allows specifying independent bounds for serialization vs deserialization:

  - `#[serde(bound(serialize = "T: MySerTrait"))]`
  - `#[serde(bound(deserialize = "T: MyDeTrait"))]`
  - `#[serde(bound(serialize = "T: MySerTrait", deserialize = "T: MyDeTrait"))]`
- ##### `#[serde(getter = "...")]`

  This is used when deriving `Serialize` for a [remote type](https://serde.rs/remote-derive.html) that has one or more private fields.
