---
url: https://docs.djangoproject.com/en/6.1/ref/models/querysets/
title: QuerySet API reference
words: 20594
---
# `QuerySet` API reference

This document describes the details of the `QuerySet` API. It builds on the material presented in the [model](https://docs.djangoproject.com/en/6.1/topics/db/models/) and [database query](https://docs.djangoproject.com/en/6.1/topics/db/queries/) guides, so you’ll probably want to read and understand those documents before reading this one.

Throughout this reference we’ll use the [example blog models](https://docs.djangoproject.com/en/6.1/topics/db/queries/#queryset-model-example) presented in the [database query guide](https://docs.djangoproject.com/en/6.1/topics/db/queries/).

## When `QuerySet`s are evaluated

Internally, a `QuerySet` can be constructed, filtered, sliced, and generally passed around without actually hitting the database. No database activity actually occurs until you do something to evaluate the queryset.

You can evaluate a `QuerySet` in the following ways:

- **Iteration.** A `QuerySet` is iterable, and it executes its database query the first time you iterate over it. For example, this will print the headline of all entries in the database:

  ```
  for e in Entry.objects.all():
      print(e.headline)
  ```
  Note: Don’t use this if all you want to do is determine if at least one result exists. It’s more efficient to use [`exists()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.exists "django.db.models.query.QuerySet.exists").
- **Asynchronous iteration.** A `QuerySet` can also be iterated over using `async for`:

  ```
  async for e in Entry.objects.all():
      results.append(e)
  ```
  Both synchronous and asynchronous iterators of QuerySets share the same underlying cache.
- **Slicing.** As explained in [Limiting QuerySets](https://docs.djangoproject.com/en/6.1/topics/db/queries/#limiting-querysets), a `QuerySet` can be sliced, using Python’s array-slicing syntax. Slicing an unevaluated `QuerySet` usually returns another unevaluated `QuerySet`, but Django will execute the database query if you use the “step” parameter of slice syntax, and will return a list. Slicing a `QuerySet` that has been evaluated also returns a list.

  Also note that even though slicing an unevaluated `QuerySet` returns another unevaluated `QuerySet`, modifying it further (e.g., adding more filters, or modifying ordering) is not allowed, since that does not translate well into SQL and it would not have a clear meaning either.
- **Pickling/Caching.** See the following section for details of what is involved when [pickling QuerySets](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#pickling-querysets). The important thing for the purposes of this section is that the results are read from the database.
- **repr().** A `QuerySet` is evaluated when you call `repr()` on it. This is for convenience in the Python interactive interpreter, so you can immediately see your results when using the API interactively.
- **len().** A `QuerySet` is evaluated when you call `len()` on it. This, as you might expect, returns the length of the result list.

  Note: If you only need to determine the number of records in the set (and don’t need the actual objects), it’s much more efficient to handle a count at the database level using SQL’s `SELECT COUNT(*)`. Django provides a [`count()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.count "django.db.models.query.QuerySet.count") method for precisely this reason.
- **list().** Force evaluation of a `QuerySet` by calling `list()` on it. For example:

  ```
  entry_list = list(Entry.objects.all())
  ```
- **bool().** Testing a `QuerySet` in a boolean context, such as using `bool()`, `or`, `and` or an `if` statement, will cause the query to be executed. If there is at least one result, the `QuerySet` is `True`, otherwise `False`. For example:

  ```
  if Entry.objects.filter(headline="Test"):
      print("There is at least one Entry with the headline Test")
  ```
  Note: If you only want to determine if at least one result exists (and don’t need the actual objects), it’s more efficient to use [`exists()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.exists "django.db.models.query.QuerySet.exists").

### Pickling `QuerySet`s

If you [`pickle`](https://docs.python.org/3/library/pickle.html#module-pickle "(in Python v3.15)") a `QuerySet`, this will force all the results to be loaded into memory prior to pickling. Pickling is usually used as a precursor to caching and when the cached queryset is reloaded, you want the results to already be present and ready for use (reading from the database can take some time, defeating the purpose of caching). This means that when you unpickle a `QuerySet`, it contains the results at the moment it was pickled, rather than the results that are currently in the database.

If you only want to pickle the necessary information to recreate the `QuerySet` from the database at a later time, pickle the `query` attribute of the `QuerySet`. You can then recreate the original `QuerySet` (without any results loaded) using some code like this:

```
>>> import pickle
>>> query = pickle.loads(s)  # Assuming 's' is the pickled string.
>>> qs = MyModel.objects.all()
>>> qs.query = query  # Restore the original 'query'.
```
The `query` attribute is an opaque object. It represents the internals of the query construction and is not part of the public API. However, it is safe (and fully supported) to pickle and unpickle the attribute’s contents as described here.

Restrictions on `QuerySet.values_list()`

If you recreate [`QuerySet.values_list()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.values_list "django.db.models.query.QuerySet.values_list") using the pickled `query` attribute, it will be converted to [`QuerySet.values()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.values "django.db.models.query.QuerySet.values"):

```
>>> import pickle
>>> qs = Blog.objects.values_list("id", "name")
>>> qs
<QuerySet [(1, 'Beatles Blog')]>
>>> reloaded_qs = Blog.objects.all()
>>> reloaded_qs.query = pickle.loads(pickle.dumps(qs.query))
>>> reloaded_qs
<QuerySet [{'id': 1, 'name': 'Beatles Blog'}]>
```
## `QuerySet` API

Here’s the formal declaration of a `QuerySet`:

*class* QuerySet(*model=None*, *query=None*, *using=None*, *hints=None*)[\[source\]](https://github.com/django/django/blob/stable/6.1.x/django/db/models/query.py#L364)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet "Link to this definition")

Usually when you’ll interact with a `QuerySet` you’ll use it by [chaining filters](https://docs.djangoproject.com/en/6.1/topics/db/queries/#chaining-filters). To make this work, most `QuerySet` methods return new querysets. These methods are covered in detail later in this section.

The `QuerySet` class has the following public attributes you can use for introspection:

ordered[\[source\]](https://github.com/django/django/blob/stable/6.1.x/django/db/models/query.py#L2036)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.ordered "Link to this definition")

`True` if the `QuerySet` is ordered — i.e. has an [`order_by()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.order_by "django.db.models.query.QuerySet.order_by") clause or a default ordering on the model. `False` otherwise.

totally\_ordered[\[source\]](https://github.com/django/django/blob/stable/6.1.x/django/db/models/query.py#L2057)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.totally_ordered "Link to this definition")

New in Django 6.1.

Returns `True` if the `QuerySet` is ordered and the ordering is deterministic. This requires that the ordering includes a field (or set of fields) that is unique and non-nullable.

For queries involving a `GROUP BY` clause, the model’s default ordering is ignored. Ordering specified via `.extra(order_by=...)` is also ignored.

db[\[source\]](https://github.com/django/django/blob/stable/6.1.x/django/db/models/query.py#L2131)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.db "Link to this definition")

The database that will be used if this query is executed now.

Note

The `query` parameter to [`QuerySet`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet "django.db.models.query.QuerySet") exists so that specialized query subclasses can reconstruct internal query state. The value of the parameter is an opaque representation of that query state and is not part of a public API.

### Methods that return new `QuerySet`s

Django provides a range of `QuerySet` refinement methods that modify either the types of results returned by the `QuerySet` or the way its SQL query is executed.

Note

These methods do not run database queries, therefore they are **safe to** **run in asynchronous code**, and do not have separate asynchronous versions.

#### `filter()`

filter(*\*args*, *\*\*kwargs*)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.filter "Link to this definition")

Returns a new `QuerySet` containing objects that match the given lookup parameters.

The lookup parameters (`**kwargs`) should be in the format described in [Field lookups](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#id4) below. Multiple parameters are joined via `AND` in the underlying SQL statement.

If you need to execute more complex queries (for example, queries with `OR` statements), you can use [Q() objects](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#q-objects) (`*args`).

`NULL` values and three-valued logic

When passing a `QuerySet` to the [`in`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#std-fieldlookup-in) lookup, ensure the subquery does not return `NULL` values. See [this note](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#null-and-three-valued-logic) for details.

#### `exclude()`

exclude(*\*args*, *\*\*kwargs*)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.exclude "Link to this definition")

Returns a new `QuerySet` containing objects that do *not* match the given lookup parameters.

The lookup parameters (`**kwargs`) should be in the format described in [Field lookups](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#id4) below. Multiple parameters are joined via `AND` in the underlying SQL statement, and the whole thing is enclosed in a `NOT()`.

This example excludes all entries whose `pub_date` is later than 2005-1-3 AND whose `headline` is “Hello”:

```
Entry.objects.exclude(pub_date__gt=datetime.date(2005, 1, 3), headline="Hello")
```
In SQL terms, that evaluates to:

```
SELECT ...
WHERE NOT (pub_date > '2005-1-3' AND headline = 'Hello')
```
This example excludes all entries whose `pub_date` is later than 2005-1-3 OR whose headline is “Hello”:

```
Entry.objects.exclude(pub_date__gt=datetime.date(2005, 1, 3)).exclude(headline="Hello")
```
In SQL terms, that evaluates to:

```
SELECT ...
WHERE NOT pub_date > '2005-1-3'
AND NOT headline = 'Hello'
```
Note the second example is more restrictive.

If you need to execute more complex queries (for example, queries with `OR` statements), you can use [`Q objects`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.Q "django.db.models.Q") (`*args`).

`NULL` values and three-valued logic

When passing a `QuerySet` to the [`in`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#std-fieldlookup-in) lookup, ensure the subquery does not return `NULL` values. See [this note](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#null-and-three-valued-logic) for details.

#### `annotate()`

annotate(*\*args*, *\*\*kwargs*)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.annotate "Link to this definition")

Annotates each object in the `QuerySet` with the provided list of [query expressions](https://docs.djangoproject.com/en/6.1/ref/models/expressions/) or [Q() objects](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#q-objects). Each object can be annotated with:

- a simple value, via `Value()`;
- a reference to a field on the model (or any related models), via `F()`;
- a boolean, via `Q()`; or
- a result from an aggregate expression (averages, sums, etc.) computed over the objects that are related to the objects in the `QuerySet`.

Each argument to `annotate()` is an annotation that will be added to each object in the `QuerySet` that is returned.

The aggregation functions that are provided by Django are described in [Aggregation Functions](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#id6) below.

Annotations specified using keyword arguments will use the keyword as the alias for the annotation. Anonymous arguments will have an alias generated for them based upon the name of the aggregate function and the model field that is being aggregated. Only aggregate expressions that reference a single field can be anonymous arguments. Everything else must be a keyword argument.

For example, if you were manipulating a list of blogs, you may want to determine how many entries have been made in each blog:

```
>>> from django.db.models import Count
>>> q = Blog.objects.annotate(Count("entry"))
# The name of the first blog
>>> q[0].name
'Blogasaurus'
# The number of entries on the first blog
>>> q[0].entry__count
42
```
The `Blog` model doesn’t define an `entry__count` attribute by itself, but by using a keyword argument to specify the aggregate function, you can control the name of the annotation:

```
>>> q = Blog.objects.annotate(number_of_entries=Count("entry"))
# The number of entries on the first blog, using the name provided
>>> q[0].number_of_entries
42
```
For an in-depth discussion of aggregation, see [the topic guide on Aggregation](https://docs.djangoproject.com/en/6.1/topics/db/aggregation/).

#### `fetch_mode()`

New in Django 6.1.

fetch\_mode(*mode*)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.fetch_mode "Link to this definition")

Returns a `QuerySet` that sets the given fetch mode for all model instances created by this `QuerySet`. The fetch mode controls on-demand loading of fields when they are accessed, such as for foreign keys and deferred fields. For example, to use the [`FETCH_PEERS`](https://docs.djangoproject.com/en/6.1/topics/db/fetch-modes/#django.db.models.FETCH_PEERS "django.db.models.FETCH_PEERS") mode to batch-load all related objects on first access:

```
from django.db import models

books = Book.objects.fetch_mode(models.FETCH_PEERS)
```
See more in the [fetch mode topic guide](https://docs.djangoproject.com/en/6.1/topics/db/fetch-modes/).

#### `select_related()`

select\_related(*\*fields*)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.select_related "Link to this definition")

Returns a `QuerySet` that will join in the named foreign-key relationships, selecting additional related objects when it executes its query. This method can be a performance booster, fetching data ahead of time rather than triggering on-demand loading through the model instances’ [fetch mode](https://docs.djangoproject.com/en/6.1/topics/db/fetch-modes/), at the cost of a more complex initial query.

The following examples illustrate the difference between plain lookups and `select_related()` lookups. Here’s standard lookup:

```
# Hits the database.
e = Entry.objects.get(id=5)

# Hits the database again to get the related Blog object.
b = e.blog
```
And here’s `select_related` lookup:

```
# Hits the database.
e = Entry.objects.select_related("blog").get(id=5)

# Doesn't hit the database, because e.blog has been prepopulated
# in the previous query.
b = e.blog
```
You can use `select_related()` with any queryset. The order of chaining with other methods isn’t important. For example, these querysets are equivalent:

```
Entry.objects.filter(pub_date__gt=timezone.now()).select_related("blog")
Entry.objects.select_related("blog").filter(pub_date__gt=timezone.now())
```
You can follow foreign keys in a similar way to querying them. If you have the following models:

```
from django.db import models


class City(models.Model):
    # ...
    pass


class Person(models.Model):
    # ...
    hometown = models.ForeignKey(
        City,
        on_delete=models.SET_NULL,
        blank=True,
        null=True,
    )


class Book(models.Model):
    # ...
    author = models.ForeignKey(Person, on_delete=models.CASCADE)
```
Then a call to `Book.objects.select_related('author__hometown').get(id=4)` will cache the related `Person` *and* the related `City`:

```
# Hits the database with joins to the author and hometown tables.
b = Book.objects.select_related("author__hometown").get(id=4)
p = b.author  # Doesn't hit the database.
c = p.hometown  # Doesn't hit the database.

# Without select_related()...
b = Book.objects.get(id=4)  # Hits the database.
p = b.author  # Hits the database.
c = p.hometown  # Hits the database.
```
You can refer to any [`ForeignKey`](https://docs.djangoproject.com/en/6.1/ref/models/fields/#django.db.models.ForeignKey "django.db.models.ForeignKey") or [`OneToOneField`](https://docs.djangoproject.com/en/6.1/ref/models/fields/#django.db.models.OneToOneField "django.db.models.OneToOneField") relation in the list of fields passed to `select_related()`.

You can also refer to the reverse direction of a [`OneToOneField`](https://docs.djangoproject.com/en/6.1/ref/models/fields/#django.db.models.OneToOneField "django.db.models.OneToOneField") in the list of fields passed to `select_related` — that is, you can traverse a [`OneToOneField`](https://docs.djangoproject.com/en/6.1/ref/models/fields/#django.db.models.OneToOneField "django.db.models.OneToOneField") back to the object on which the field is defined. Instead of specifying the field name, use the [`related_name`](https://docs.djangoproject.com/en/6.1/ref/models/fields/#django.db.models.ForeignKey.related_name "django.db.models.ForeignKey.related_name") for the field on the related object.

If you need to clear the list of related fields added by past calls of `select_related` on a `QuerySet`, you can pass `None` as a parameter:

```
>>> without_relations = queryset.select_related(None)
```
Chaining `select_related` calls works in a similar way to other methods - that is that `select_related('foo', 'bar')` is equivalent to `select_related('foo').select_related('bar')`.

There may be some situations where you wish to call `select_related()` with a lot of related objects, or where you don’t know all of the relations. In these cases it is possible to call `select_related()` with no arguments. This will follow all non-null foreign keys it can find - nullable foreign keys must be specified. This is not recommended in most cases as it is likely to make the underlying query more complex, and return more data, than is actually needed.

Deprecated since version 6.1: Calling `select_related()` with no arguments is deprecated, and support for it will be removed in Django 2028. Specify the fields to fetch instead.

#### `prefetch_related()`

prefetch\_related(*\*lookups*)[¶](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.prefetch_related "Link to this definition")

Returns a `QuerySet` that will automatically retrieve the given lookups, each in one extra batch query. Prefetching is a way to optimize database access when you know you’ll be accessing related objects later, so you can avoid triggering the on-demand loading behavior of the model instances’ [fetch mode](https://docs.djangoproject.com/en/6.1/topics/db/fetch-modes/).

This method has a similar purpose to [`select_related()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.select_related "django.db.models.query.QuerySet.select_related"), in that both are designed to eagerly fetch related objects. However, they work in different ways.

`select_related` works by creating an SQL join and including the fields of the related object in the `SELECT` statement. For this reason, `select_related` gets the related objects in the same database query. However, to avoid the much larger result set that would result from joining across a ‘many’ relationship, `select_related` is limited to single-valued relationships - foreign key and one-to-one.

`prefetch_related`, on the other hand, does a separate lookup for each relationship, and does the ‘joining’ in Python. This allows it to prefetch many-to-many, many-to-one, and [`GenericRelation`](https://docs.djangoproject.com/en/6.1/ref/contrib/contenttypes/#django.contrib.contenttypes.fields.GenericRelation "django.contrib.contenttypes.fields.GenericRelation") objects which cannot be done using `select_related`, in addition to the foreign key and one-to-one relationships that are supported by `select_related`. It also supports prefetching of [`GenericForeignKey`](https://docs.djangoproject.com/en/6.1/ref/contrib/contenttypes/#django.contrib.contenttypes.fields.GenericForeignKey "django.contrib.contenttypes.fields.GenericForeignKey"), however, the queryset for each `ContentType` must be provided in the `querysets` parameter of [`GenericPrefetch`](https://docs.djangoproject.com/en/6.1/ref/contrib/contenttypes/#django.contrib.contenttypes.prefetch.GenericPrefetch "django.contrib.contenttypes.prefetch.GenericPrefetch").

For example, suppose you have these models:

```
from django.db import models


class Topping(models.Model):
    name = models.CharField(max_length=30)


class Pizza(models.Model):
    name = models.CharField(max_length=50)
    toppings = models.ManyToManyField(Topping)

    def __str__(self):
        return "%s (%s)" % (
            self.name,
            ", ".join(topping.name for topping in self.toppings.all()),
        )
```
and run:

```
>>> Pizza.objects.all()
<QuerySet [<Pizza: Hawaiian (ham, pineapple)>, <Pizza: Seafood (prawns, smoked salmon)>, ...]>
```
The problem with this is that every time `Pizza.__str__()` asks for `self.toppings.all()` it has to query the database, so `Pizza.objects.all()` will run a query on the Toppings table for **every** item in the Pizza `QuerySet`.

We can reduce to just two queries using `prefetch_related`:

```
>>> Pizza.objects.prefetch_related("toppings")
```
This implies a `self.toppings.all()` for each `Pizza`; now each time `self.toppings.all()` is called, instead of having to go to the database for the items, it will find them in a prefetched `QuerySet` cache that was populated in a single query.

That is, all the relevant toppings will have been fetched in a single query, and used to make `QuerySet` instances that have a pre-filled cache of the relevant results; these are then used in the `self.toppings.all()` calls.

The additional queries in `prefetch_related()` are executed after the `QuerySet` has begun to be evaluated and the primary query has been executed.

Note that there is no mechanism to prevent another database query from altering the items in between the execution of the primary query and the additional queries, which could produce an inconsistent result. For example, if a `Pizza` is deleted after the primary query has executed, its toppings will not be returned in the additional query, and it will seem like the pizza has no toppings:

```
>>> Pizza.objects.prefetch_related("toppings")
#  "Hawaiian" Pizza was deleted in another shell.
<QuerySet [<Pizza: Hawaiian ()>, <Pizza: Seafood (prawns, smoked salmon)>]>
```
If you have an iterable of model instances, you can prefetch related attributes on those instances using the [`prefetch_related_objects()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.prefetch_related_objects "django.db.models.prefetch_related_objects") function.

Note that the result cache of the primary `QuerySet` and all specified related objects will then be fully loaded into memory. This changes the typical behavior of a `QuerySet`, which normally tries to avoid loading all objects into memory before they are needed, even after a query has been executed in the database.

Note

Remember that, as always with `QuerySet` objects, any subsequent chained methods which imply a different database query will ignore previously cached results, and retrieve data using a fresh database query. So, if you write the following:

```
>>> pizzas = Pizza.objects.prefetch_related("toppings")
>>> [list(pizza.toppings.filter(spicy=True)) for pizza in pizzas]
```
…then the fact that `pizza.toppings.all()` has been prefetched will not help you. The `prefetch_related('toppings')` implied `pizza.toppings.all()`, but `pizza.toppings.filter()` is a new and different query. The prefetched cache can’t help here; in fact it hurts performance, since you have done a database query that you haven’t used. So use this feature with caution!

Also, if you call the database-altering methods [`add()`](https://docs.djangoproject.com/en/6.1/ref/models/relations/#django.db.models.fields.related.RelatedManager.add "django.db.models.fields.related.RelatedManager.add"), [`create()`](https://docs.djangoproject.com/en/6.1/ref/models/relations/#django.db.models.fields.related.RelatedManager.create "django.db.models.fields.related.RelatedManager.create"), [`remove()`](https://docs.djangoproject.com/en/6.1/ref/models/relations/#django.db.models.fields.related.RelatedManager.remove "django.db.models.fields.related.RelatedManager.remove"), [`clear()`](https://docs.djangoproject.com/en/6.1/ref/models/relations/#django.db.models.fields.related.RelatedManager.clear "django.db.models.fields.related.RelatedManager.clear") or [`set()`](https://docs.djangoproject.com/en/6.1/ref/models/relations/#django.db.models.fields.related.RelatedManager.set "django.db.models.fields.related.RelatedManager.set"), on a [`RelatedManager`](https://docs.djangoproject.com/en/6.1/ref/models/relations/#django.db.models.fields.related.RelatedManager "django.db.models.fields.related.RelatedManager"), any prefetched cache for the relation will be cleared.

You can also use the normal join syntax to do related fields of related fields. Suppose we have an additional model to the example above:

```
class Restaurant(models.Model):
    pizzas = models.ManyToManyField(Pizza, related_name="restaurants")
    best_pizza = models.ForeignKey(
        Pizza, related_name="championed_by", on_delete=models.CASCADE
    )
```
The following are all legal:

```
>>> Restaurant.objects.prefetch_related("pizzas__toppings")
```
This will prefetch all pizzas belonging to restaurants, and all toppings belonging to those pizzas. This will result in a total of 3 database queries - one for the restaurants, one for the pizzas, and one for the toppings.

```
>>> Restaurant.objects.prefetch_related("best_pizza__toppings")
```
This will fetch the best pizza and all the toppings for the best pizza for each restaurant. This will be done in 3 database queries - one for the restaurants, one for the ‘best pizzas’, and one for the toppings.

The `best_pizza` relationship could also be fetched using `select_related` to reduce the query count to 2:

```
>>> Restaurant.objects.select_related("best_pizza").prefetch_related("best_pizza__toppings")
```
Since the prefetch is executed after the main query (which includes the joins needed by `select_related`), it is able to detect that the `best_pizza` objects have already been fetched, and it will skip fetching them again.

Chaining `prefetch_related` calls will accumulate the lookups that are prefetched. To clear any `prefetch_related` behavior, pass `None` as a parameter:

```
>>> non_prefetched = qs.prefetch_related(None)
```
One difference to note when using `prefetch_related` is that objects created by a query can be shared between the different objects that they are related to i.e. a single Python model instance can appear at more than one point in the tree of objects that are returned. This will normally happen with foreign key relationships. Typically this behavior will not be a problem, and will in fact save both memory and CPU time.

While `prefetch_related` supports prefetching `GenericForeignKey` relationships, the number of queries will depend on the data. Since a `GenericForeignKey` can reference data in multiple tables, one query per table referenced is needed, rather than one query for all the items. There could be additional queries on the `ContentType` table if the relevant rows have not already been fetched.

`prefetch_related` in most cases will be implemented using an SQL query that uses the ‘IN’ operator. This means that for a large `QuerySet` a large ‘IN’ clause could be generated, which, depending on the database, might have performance problems of its own when it comes to parsing or executing the SQL query. Always profile for your use case!

If you use `iterator()` to run the query, `prefetch_related()` calls will only be observed if a value for `chunk_size` is provided.

You can use the [`Prefetch`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.Prefetch "django.db.models.Prefetch") object to further control the prefetch operation.

In its simplest form `Prefetch` is equivalent to the traditional string based lookups:

```
>>> from django.db.models import Prefetch
>>> Restaurant.objects.prefetch_related(Prefetch("pizzas__toppings"))
```
You can provide a custom queryset with the optional `queryset` argument. This can be used to change the default ordering of the queryset:

```
>>> Restaurant.objects.prefetch_related(
...     Prefetch("pizzas__toppings", queryset=Toppings.objects.order_by("name"))
... )
```
Or to call [`select_related()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.select_related "django.db.models.query.QuerySet.select_related") when applicable to reduce the number of queries even further:

```
>>> Pizza.objects.prefetch_related(
...     Prefetch("restaurants", queryset=Restaurant.objects.select_related("best_pizza"))
... )
```
You can also assign the prefetched result to a custom attribute with the optional `to_attr` argument. The result will be stored directly in a list.

This allows prefetching the same relation multiple times with a different `QuerySet`; for instance:

```
>>> vegetarian_pizzas = Pizza.objects.filter(vegetarian=True)
>>> Restaurant.objects.prefetch_related(
...     Prefetch("pizzas", to_attr="menu"),
...     Prefetch("pizzas", queryset=vegetarian_pizzas, to_attr="vegetarian_menu"),
... )
```
Lookups created with custom `to_attr` can still be traversed as usual by other lookups:

```
>>> vegetarian_pizzas = Pizza.objects.filter(vegetarian=True)
>>> Restaurant.objects.prefetch_related(
...     Prefetch("pizzas", queryset=vegetarian_pizzas, to_attr="vegetarian_menu"),
...     "vegetarian_menu__toppings",
... )
```
Using `to_attr` is recommended when filtering down the prefetch result as it is less ambiguous than storing a filtered result in the related manager’s cache:

```
>>> queryset = Pizza.objects.filter(vegetarian=True)
>>>
>>> # Recommended:
>>> restaurants = Restaurant.objects.prefetch_related(
...     Prefetch("pizzas", queryset=queryset, to_attr="vegetarian_pizzas")
... )
>>> vegetarian_pizzas = restaurants[0].vegetarian_pizzas
>>>
>>> # Not recommended:
>>> restaurants = Restaurant.objects.prefetch_related(
...     Prefetch("pizzas", queryset=queryset),
... )
>>> vegetarian_pizzas = restaurants[0].pizzas.all()
```
Custom prefetching also works with single related relations like forward `ForeignKey` or `OneToOneField`. Generally you’ll want to use [`select_related()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.select_related "django.db.models.query.QuerySet.select_related") for these relations, but there are a number of cases where prefetching with a custom `QuerySet` is useful:

- You want to use a `QuerySet` that performs further prefetching on related models.
- You want to prefetch only a subset of the related objects.
- You want to use performance optimization techniques like deferring fields, for example, via [`defer()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.defer "django.db.models.query.QuerySet.defer") or [`only()`](https://docs.djangoproject.com/en/6.1/ref/models/querysets/#django.db.models.query.QuerySet.only "django.db.models.query.QuerySet.only"):

  ```
  >>> queryset = Pizza.objects.only("name")
  >>>
  >>> restaurants = Restaurant.objects.prefetch_related(
  ...     Prefetch("best_pizza", queryset=queryset)
  ... )
  ```
When using multiple databases, `Prefetch` will respect your choice of database. If the inner query does not specify a database, it will use the database selected by the outer query. All of the following are valid:

```
>>> # Both inner and outer queries will use the 'replica' database
>>> Restaurant.objects.prefetch_related("pizzas__toppings").using("replica")
>>> Restaurant.objects.prefetch_related(
...     Prefetch("pizzas__toppings"),
... ).using("replica")
>>>
>>> # Inner will use the 'replica' database; outer will use 'default' database
>>> Restaurant.objects.prefetch_related(
...     Prefetch("pizzas__toppings", queryset=Toppings.objects.using("replica")),
... )
>>>
>>> # Inner will use 'replica' database; outer will use 'cold-storage' database
>>> Restaurant.objects.prefetch_related(
...     Prefetch("pizzas__toppings", queryset=Toppings.objects.using("replica")),
... ).using("cold-storage")
```
Note

The ordering of lookups matters.

Take the following examples:

```
>>> prefetch_related("pizzas__toppings", "pizzas")
```
This works even though it’s unordered because `'pizzas__toppings'` already contains all the needed information, therefore the second argument `'pizzas'` is actually redundant.

```
>>> prefetch_related("pizzas__toppings", Prefetch("pizzas", queryset=Pizza.objects.all()))
```
This will raise a `ValueError` because of the attempt to redefine the queryset of a previously seen lookup. Note that an implicit queryset was created to traverse `'pizzas'` as part of the `'pizzas__toppings'` lookup.

```
>>> prefetch_related("pizza_list__toppings", Prefetch("pizzas", to_attr="pizza_list"))
```
This will trigger an `AttributeError` because `'pizza_list'` doesn’t exist yet when `'pizza_list__toppings'` is being processed.

This consideration is not limited to the use of `Prefetch` objects. Some advanced techniques may require that the lookups be performed in a specific order to avoid creating extra queries; therefore it’s recommended to always carefully order `prefetch_related` arguments.

### Operators that return new `QuerySet`s

Combined querysets must use the same model.

#### AND (`&`)

Combines two `QuerySet`s using the SQL `AND` operator in a manner similar to chaining filters.

The following are equivalent:

```
Model.objects.filter(x=1) & Model.objects.filter(y=2)
Model.objects.filter(x=1).filter(y=2)
```
SQL equivalent:

```
SELECT ... WHERE x=1 AND y=2
```
#### OR (`|`)

Combines two `QuerySet`s using the SQL `OR` operator.

The following are equivalent:

```
Model.objects.filter(x=1) | Model.objects.filter(y=2)
from django.db.models import Q

Model.objects.filter(Q(x=1) | Q(y=2))
```
SQL equivalent:

```
SELECT ... WHERE x=1 OR y=2
```
`|` is not a commutative operation, as different (though equivalent) queries may be generated.

#### XOR (`^`)

Combines two `QuerySet`s using the SQL `XOR` operator. A `XOR` expression matches rows that are matched by an odd number of operands.

The following are equivalent:

```
Model.objects.filter(x=1) ^ Model.objects.filter(y=2)
from django.db.models import Q

Model.objects.filter(Q(x=1) ^ Q(y=2))
```
SQL equivalent:

```
SELECT ... WHERE x=1 XOR y=2
```
Note

`XOR` is natively supported on MariaDB and MySQL. On other databases, `x ^ y ^ ... ^ z` is converted to an equivalent:

```
(x OR y OR ... OR z) AND
1=MOD(
    (CASE WHEN x THEN 1 ELSE 0 END) +
    (CASE WHEN y THEN 1 ELSE 0 END) +
    ...
    (CASE WHEN z THEN 1 ELSE 0 END),
    2
)
```
