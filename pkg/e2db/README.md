# e2db

e2db is an experimental abstraction layer built on top of etcd providing an ORM-like interface. It is heavily influenced by the design of [storm](https://github.com/asdine/storm).

## Table of Contents

- [Getting Started](#getting-started)
  - [Open a database](#open-a-database)
  - [Configuration](#configuration)
  - [Error handling](#error-handling)
- [Usage](#usage)
  - [Define a table](#define-a-table)
  - [Create a table object](#create-a-table-object)
  - [Insert a new object](#insert-a-new-object)
  - [Fetch one object](#fetch-one-object)
  - [Fetch multiple objects](#fetch-multiple-objects)
  - [Fetch multiple objects sorted by index](#fetch-multiple-objects-sorted-by-index)
  - [Delete multiple objects](#delete-multiple-objects)
  - [Drop a table](#drop-a-table)
- [Advanced Usage](#advanced-usage)
  - [Transactions](#transactions)
  - [Query filtering](#query-filtering)
  - [Distributed locks](#distributed-locks)
  - [Table encryption](#table-encryption)

## Getting Started

### Open a database

```go
import (
    "log"

    "github.com/criticalstack/e2d/pkg/e2db"
)

func main() {
    db, err := e2db.New(&e2db.Config{
        ClientAddr: ":2379",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
}
```

Since e2db relies on the etcd clientv3, the connection must call the `Close()` method when finished.

### Configuration

| Name | Description |
| --- | --- |
| ClientAddr | The address for the etcd client server. This should not specify the URL parts like scheme as that will be built automatically. |
| Namespace | A namespace can be provided to transparently prefix all keys and isolate them from other non-e2db keys that may be in the database. |
| CertFile | Client cert |
| KeyFile | Client key |
| CAFile | Trusted CA cert |

To connect to an etcd server that has mTLS client authentication, all of the following values must be provided: `CertFile`, `KeyFile`, and `CAFile`. This will also ensure that the appropriate scheme of https is used when generating the `ClientURL` from the provided `ClientAddr`.

### Error handling

e2db uses the package [github.com/pkg/errors](https://github.com/pkg/errors) for handling errors. For example, a query that does not return rows will returned the wrapped `error` type `ErrNoRows`, so the function [errors.Cause](https://godoc.org/github.com/pkg/errors#Cause) must be called to get the underlying type for comparison:

```go
if errors.Cause(err) == e2db.ErrNoRows {
    // handle ErrNoRows error
}
```

## Usage

### Define a table

Table schema is defined by defining structs:

```go
type User struct {
    ID       int `e2db:"increment"`
    Name     string `e2db:"index"`
    Email    string `e2db:"unique"`
    Role     string `e2db:"index,required"`
    Enabled  bool `e2db:"index"`
    Created  time.Time
}
```

Struct tags provide flexible ways of defining indexes or constraints:

| Tag | Description |
| --- | --- |
| id | Defines a field as the primary key |
| increment | Defines a field as the primary key and automatically increments the value starting from 1 |
| index | Creates an index for the field value |
| unique | Creates an index for the field value along with a unique constraint |
| required | Field must have a value provided |

Table metadata is stored the first time data is added for a table to ensure that other operations will not violate the table schema that has been established. Other important table-specific metadata includes table-level locks and auto-incrementing field information.

Index metadata is stored along with the table also and is modified in the same operation as the data (i.e. the cost of building the index is amortized with the operation).

So internally the table starts look like this:

| Key | Value Description |
| --- | --- |
|`/<namespace>/User/_table` | gob-encoded table metadata |
|`/<namespace>/User/_table/ID/last` | last increment value |
|`/<namespace>/User/_table/lock` | N/A |
|`/<namespace>/User/_index/Name/<value>/<pk>` | full key for the indexed item |
|`/<namespace>/User/_index/Email/<value>` | full key for the indexed item |
|`/<namespace>/User/_index/Role/<value>/<pk>` | full key for the indexed item |
|`/<namespace>/User/_index/Created/<value>/<pk>` | full key for the indexed item |

where an index key/value exists for every item that is indexed. In other words, for a table with schema like `User`, 5 rows will result in 20 key/value pairs being stored given the above configuration for `User` to satisfy building all the defined indexes.

### Create a table object

Creating a table object can be achieved by passing in a concrete type for the defined table:

```go
users := db.Table(new(User))
```

This can now be used as a reference to refer to that table. Under the hood, e2db is using this to lazily store and check any subsequent operations to match an existing schema (stored in the table metadata) with the one passed in. Checking this schema ensures that a table schema other than one already defined for a table will result in an error.

### Insert a new object

```go
user := User{
    Name: "Smoot Wellington",
    Email: "smoot.wellington@hotmail.com",
    Role: "user",
    Enabled: true,
    Created: time.Now()
}
err := users.Insert(&user)
```

In this case there is an auto-incrementing field for `ID` so after the call to `Insert` the value for `user.ID` will be set (before it will be the zero value).

### Fetch one object

Using the tag `id` or `increment` designates a field to be the tables primary key:

```go
var u User
err := users.Find("ID", 1, &u)
```

Getting a single object back by index is accomplished the same way:

```go
err := users.Find("Name", "Smoot Wellington", &u)
```

### Fetch multiple objects

```go
var u []User
err := users.Find("Role", "user", &u)
```

Or simply fetch all objects in a table:

```go
err := users.All(&u)
```

### Fetch multiple objects sorted by index

To sort by index in ascending order:

```go
var u []User
err := users.OrderBy("Name").Find("Role", "user", &u)
```

For descending, simply call `Reverse()`:

```go
err := users.OrderBy("Name").Reverse().Find("Role", "user", &u)
```

### Update an object

```go
user.Role = "admin"
err := users.Update(&user)
```

### Delete one object

```go
err := users.Delete("ID", 1)
```

### Delete multiple objects

```go
err := users.Delete("Role", "user")
```

### Drop a table

Table metadata is stored in the database to ensure that the types match before an operation is performed.  If a table has changed or no longer needed it might need to be dropped so a new table can replace it:

```go
err := users.Drop()
```

This can be used to help migrate from one schema version to another.

## Advanced Usage

### Transactions

Transactions can be used to reduce the amount of table locking that is occurring. This is helpful when doing bulk insert/update/delete operations:

```go
err := users.Tx(func(tx *Tx) error {
    for _, row := range rows {
        if err := tx.Insert(row); err != nil {
	    return err
	}
    }
    return nil
})
```

In this case, only one lock will be acquired for the duration of the transaction.

### Query filtering

```go
err := users.Filter(q.Eq("Enabled", false)).Find("Role", "user", &u)

err := users.Filter(
    q.And(
        q.Eq("Enabled", false),
	q.Not("Name", "superadmin")
    )
).Find("Role", "user", &u)

err := users.Limit(5).Find("Role", "user", &u)
```

### Distributed locks

Distributed locking is a powerful feature made possible by etcd. Arbitrary locks can be established based upon the key string passed to `db.Lock()`, which allows for any node using e2db to synchronize.

```go
func syncSomething() error {
    unlock, err := db.Lock("sync/something", 30 * time.Second)
    if err != nil {
        return err
    }
    defer unlock()

    // do stuff

    return nil
}
```

An easier way to coordinate with distributed locks is simply racing for new object creation. This ends up being very useful in situations where, for example, you have multiple machines that need to share the same TLS cert/key pair for a web application. Something like this could be done to ensure that only the first machine that won the race for the lock will generate the TLS cert/key and then store that in e2db for the other instances to use:

```go
type SharedFile struct {
    Path string `e2db:"id"`
    Mode os.FileMode
    Data []byte
}

err := db.Table(new(SharedFile)).Tx(func(tx *e2db.Tx) error {
    var files []*Files
    if err := tx.All(&files); err != nil {
        if errors.Cause(err) != e2db.ErrNoRows {
            return err
        }

        // If this is the first machine the TLS cert/key files won't exist, so
        // we must create them. This will only ever happen once.
        cert, key, err := generateTLS()
        if err != nil {
            return err
        }
        files = append(files, &SharedFile{"/tls.crt", 0600, cert})
        files = append(files, &SharedFile{"/tls.key", 0600, key})
    }

    // write the files to disk and insert into the SharedFile table
    for _, f := range files {
        if err := tx.Insert(f); err != nil {
            return err
        }
        if err := ioutil.WriteFile(f.Path, f.Data, f.Mode); err != nil {
            return err
        }
    }
    return nil
})
```

### Table encryption

Table objects can optionally be encrypted with AES-256 GCM.

```go
err := db.Table(new(User), e2db.WithEncryption("mySecretKey"))
```

This will encrypt any objects that are stored in this table, however, there are few caveats for usage:
 * No table metadata is stored to distinguish between encrypted/unecrypted objects, so one must be careful when setting up table encryption on a client.
 * Table metadata and indexes are not encrypted. The object is encrypted/signed with strong encryption, but the table metadata is plaintext and indexes are non-cryptographically hashed. Indexes in e2db use sha512-256, so while not plaintext, they are not cryptographically secure. This just means that using tags like index or unique should not be used on data that should be kept secret.
 * This feature is only helpful in very very specific use cases. Standard encryption-at-rest procedures should be considered before using e2db table encryption.
