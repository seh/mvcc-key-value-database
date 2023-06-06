.. role:: cmdflag(code)
.. role:: command(emphasis)
.. role:: declaration(code)
.. role:: field(code)
.. role:: file(emphasis)
.. role:: httpmethod(code)
.. role:: library(emphasis)
.. role:: method(code)
.. role:: package(emphasis)
.. role:: term(emphasis)
.. role:: tool(emphasis)
.. role:: type(code)
.. role:: urlpath(emphasis)

=======================
MVCC Key-Value Database
=======================

.. External links
.. _bazel: https://bazel.build/docs
.. _Gazelle tool:
.. _gazelle: https://github.com/bazelbuild/bazel-gazelle
.. |Go import declarations| replace:: Go :declaration:`import` declarations
.. _Go import declarations: https://go.dev/ref/spec#Import_declarations
.. _the Go programming language:
.. _go spec: https://go.dev/ref/spec
.. |the go tool| replace:: the :tool:`go` tool
.. _the go tool:
.. _go: https://pkg.go.dev/cmd/go
.. _multiversion concurrency control (MVCC):
.. _mvcc: https://en.wikipedia.org/wiki/Multiversion_concurrency_control

Store byte vector values in experimental key-value database using `multiversion concurrency control (MVCC)`_, allowing many readers to proceed without waiting for any writers to finish with their changes, using HTTP clients to interact with the server.

.. contents:: :depth: 2

-----

Overview
========

Database Design
---------------

The database stores its records only in memory, organized in a manner intended to reduce the coordination delay imposed on many clients vying to read and write at the same time. The top-level storage—represented by the Go type :type:`db.ShardedStore`—uses an array of 512 Go :type:`map` values to track groups of records in separate :term:`shards`. Callers can specify a particular :term:`projection function` to determine into which shard a given record key will fall. The default projection function is from the Go standard library's :package:`hash.maphash` `package <https://pkg.go.dev/hash/maphash>`__, but others from third-party libraries would likely serve even better. Splitting up the records into these :term:`shards` reduces the likelihood that readers and writers for two different records will need to coordinate and avoid interfering with one another.

Once a given record key lands us into a :type:`db.recordMap`, we need to accommodate multiple readers and writers digging in deeper:

- Look up a record key in the :field:`recordsByKey` field's :type:`map` to find an existing :type:`versionedRecord`, which may or may contain any valid versions for the record.
- When trying to insert a record, if no :type:`versionedRecord` exists in the :field:`recordsByKey` field's :type:`map`, add a new one to start storing new versions.

These two needs conflict: We can't allow simultaneous reading and writing of these :type:`map`\s. We must employ some form of :term:`locking` to exclude mutually these concurrent operations.

Reader/Writer Locks
^^^^^^^^^^^^^^^^^^^

Though the Go standard library offers the :type:`sync.RWMutex` `type <https://pkg.go.dev/sync#RWMutex>`__ to accommodate groups of readers vying for access against a presumably smaller set of writers, it is difficult to honor a :type:`context.Context`'s "done" status to govern how long a caller will wait trying to acquire a `read <https://pkg.go.dev/sync#RWMutex.RLock>`__ or `write <https://pkg.go.dev/sync#RWMutex.Lock>`__ lock. The :type:`db.Transaction` type's interface uses :type:`context.Context` parameters throughout, indicating its conscientious effort to interrupt long-running operations upon request. To that end, I introduced the :type:`db.rwMutex` type for use in guarding our :type:`map`\s.

This :type:`db.rwMutex` type uses a pair of channels to model requests and claims by calling readers and writers. It can then offer its :method:`TryLockUntil` and :method:`TryRLockUntil` methods that accept a :type:`context.Context` parameter that limit how long the call will block waiting to acquire the respective kind of lock. Unlike :type:`sync.RWMutex`, though, :type:`db.rwMutex` does *not* ensure that a waiting writer will forestall admitting any more readers. Instead, it's possible that waiting writers will be :term:`starved` as newly arriving readers continue jumping in line ahead of them through the already open gate.

I have not yet tested the performance difference between this :type:`db.rwMutex` type and :type:`sync.RWMutex`. I expect that the former type imposes a longer delay. The :type:`db.shardedStoreTransaction` type's methods surrender both the reading and writing locks as quickly as possible after acquiring them:

- lock for reading, read an entry from a :type:`map`, then unlock,
- and only if there was no such entry and we're trying to insert a new record, lock for writing, insert the :type:`map` entry, then unlock.

Allowing More Simultaneous Access
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

Once any two callers have a :type:`versionedRecord` value in hand—new being passed the need for locking—they can proceed with their reading and writing attempts with no further locking. The database stores multiple versions for each record, with each version indicating the first or earliest transaction in which it's valid as well as an optional latest transaction in which it's no longer valid, establishing a half-open range of a validity period. Retaining these multiple versions allows both more freedom for concurrent reading and writing and better detection and control of concurrent attempts at writing that would conflict. This is a simple implementation of :term:`multiversion concurrency control`, or :term:`MVCC`.

When attempting to record new versions within a transaction, the mutation procedure adds a latest version that lacks that earliest valid transaction. This lack of a start of the validity period acts as a :term:`sentinel value` to indicate that the record version is merely :term:`pending` and not yet committed. A pending record version may already include a latest bound on its validity period to indicate that it's a deletion. Attempts to read that pending record within the same transaction would find the record effectively absent.

These pending records are effectively invisible to other concurrent readers. They continue to see the database as it was as of when their transaction began, offering a form of :term:`snapshot isolation`. Only if they then attempt to write to a record for which another writer has already created a pending version will they collide; the later attempt fails with the Go error type :type:`db.ErrTransactionInConflict`.

There are also cases where a reader finds a record version with a validity period that includes the current transaction, but that ends in a later transaction: The record had been mutated or deleted "in the future" from the current transaction's perspective. In that situation, the current transaction is not allowed to propose changes to this record, because it may be acting on observations that were true and conclusions that were justified in the past, but would no longer be appropriate for the later committed state of the database.

The database uses :term:`atomic`, :term:`lock-free` operations to inspect and mutate these in-memory structures. Doing so reduces the delay that concurrent callers would likely suffer with other lock-based techniques. In trade, though, this lock-free techniques makes some required coordination more difficult to accomplish. Removing all the traffic lights makes it harder to stop traffic.


HTTP-based Interface
====================

The HTTP server uses a simple interface in order to allow easy use of most HTTP clients—especially readily available tools like :tool:`curl` and :tool:`wget`. To that end, it uses text strings for record keys and values, even though the database's Go programming interface can accommodate arbitrary byte vectors for each.

The server accepts following operations:

- :urlpath:`/record/{key}`

  - | :httpmethod:`DELETE`
    | Delete an existing record with the given key.
    | Form parameters:

    - :field:`if-absent` (optional: :code:`abort` (default) or :code:`ignore`)

  - | :httpmethod:`GET`
    | Retrieve an existing record with the given key.

  - | :httpmethod:`POST`
    | Create a new record with the given key and value.
    | Form parameters:

    - :field:`value`

  - | :httpmethod:`PUT`
    | Update an existing record with the given key and new value.
    | Form parameters:

    - :field:`if-absent` (optional: :code:`abort` (default), :code:`insert`, or :code:`ignore`)
    - :field:`value`

- :urlpath:`/records/batch`

  - | :httpmethod:`POST`
    | Ensure afterward that any number of records with the given keys are either present with the given value or absent, effected by inserting, updating, or deleting records as necessary. Either all the required changes commit successfully or none of them do.
    | Form parameters:

    - :field:`absent` (optional: keys of records of which to ensure are absent)
    - :field:`bound` (optional: keys and values to which to ensure records are bound, written with the key surrounded by a delimiter character, e.g. :code:`:k1:abcd` or :code:`|k1|abcd`)

As with the comparable :tool:`etcd` server, `using an application protocol like gRPC <https://etcd.io/docs/v3.5/learning/api/>`__\—as opposed to :tool:`etcd`'s `earlier v2 API <https://etcd.io/docs/v2.3/api/#key-space-operations>`__\—would allow for more direct use of byte vectors. Using JSON to convey response messages and possibly request parameters might also be more fruitful for clients.


Building
========

The HTTP server program is written in `the Go programming language`_, so you can build it using either the |the go tool|_ directly or `Bazel <bazel_>`__ to integrate it with other generation and compilation needs.

The :tool:`go` tool
-------------------

From this repository's root directory, invoke the following command to build the server program:

.. code:: shell

    go build ./cmd/server

When successful, that command will produce an executable file named :file:`server` in your working directory.

Bazel
-----

From any directory within this repository, invoke the following command to build the server program using the Bazel configuration:

.. code:: shell

    bazel build //cmd/server

To locate the executable file produced by a successful invocation, invoke the following command:

.. code:: shell

    bazel cquery --output=files //cmd/server

If you've modified the source code such by adding or removing Go source files, or adding or removing |Go import declarations|_, be sure to use Bazel's `Gazelle tool`_ to update the configuration files accordingly:

.. code:: shell

    bazel run //:gazelle


Running
=======

When you want to run the program, you can `build it first <Building_>`_ and invoke the resulting executable file:

.. code:: shell

    ./server

Invoked like that with no command-line flags, the server listens on all network interfaces on port 80, accepting connections and serving requests over unencrypted HTTP. You can specify a different network address and on which to listen with the :cmdflag:`--server-address` and :cmdflag:`--server-port` command-line flags, respectively.

.. code:: shell

    ./server \
      --server-address=127.0.0.1 \
      --server-port=8080

If you'd like serve requests over HTTPS instead, specify files containing an X.509 serving certificate and its accompanying private key:

.. code:: shell

    ./server \
      --tls-cert-file=/public/server.crt \
      --tls-private-key-file=/private/server.key

When serving over HTTPS like this, the server listens on port 443 by default rather than port 80.

Note that it is also possible to have Bazel ensure that the program is built per the latest source code changes, and then run it:

.. code:: shell

    bazel run -- //cmd/server

That can be useful during active development to preclude running the previously built program without taking recent changes into account.


Outstanding Liabilities
=======================

I wrote this program in haste, and dedicated more time than prescribed, but even so had to leave it with several apparent deficiencies that follow.

- The "vacuum" procedure is not implemented, so the database can only grow, even as callers delete the records it stores. There are two levels at which this procedure could collect garbage that accumulates within the database:

  - Record versions (of type :type:`db.recordVersion`) that are valid only in transactions older than the oldest live transaction.
  - Versioned records (of type :type:`db.versionedRecord`) referenced by entries in each :type:`db.recordMap`'s :field:`recordsByKey` field that represent absent records (these either having been deleted or orphaned during insertion by aborted transactions).

  In order to collect the second kind of garbage, this procedure would need to delete entries from a Go :type:`map` value while other writers may be manipulating record versions linked from that same condemned :type:`db.versionedRecord`. We will likely need another field or two for bookkeeping in each :type:`db.versionedRecord` to allow the "vacuum" procedure to delay such concurrent mutation or detect its occurrence and transplant the revived versioned record chain into a replacement :type:`db.versionedRecord` value. Coordinating those competing efforts without imposing too much delay on writers will be challenging.

  There's some attempt at tracking the oldest extant transaction ID in the :type:`db.transactionState` type's :method:`recordFinished` method, but it's doomed. Using a probabilistic structure like a :term:`Bloom filter` to track which IDs have completed—discarding and rebuilding it occasionally—may help here.

- The ownership policy for byte vectors returned for record values by the :method:`(*db.shardedStoreTransaction).Get` method is vague. Within a transaction in which the target record was newly inserted or updated, subsequent calls to the :method:`(*db.shardedStoreTransaction).Update` method may replace the record value in place, which would still be visible through the byte slice returned by an earlier call to :method:`(*db.shardedStoreTransaction).Get`. Returning a copy of the byte vector—or demanding a destination slice into which to copy the value—would be safer, but would impose a slight performance tax on callers that don't need that protection.

- There are many cases in which concurrent transactions attempting to change the same record will suffer calls to the :method:`(*db.shardedStoreTransaction).Delete`, :method:`(*db.shardedStoreTransaction).Insert`, :method:`(*db.shardedStoreTransaction).Update`, and :method:`(*db.shardedStoreTransaction).Upsert` methods failing with :type:`ErrTransactionInConflict`, where those calls might succeed without interference if attempted again immediately afterward in a later transaction. We could introduce automatic retry policies that would give up after either some maximum number of failed attempts or after a :type:`context.Context` reports that it's done.

- Transactions each have an ID, and we assume that ID increase monotonically over time. Given that we represent transaction IDs as 64-bit-wide unsigned integers, at some point we'll saturate those values and overflow back down to zero, appearing to zoom back in time. As written the program detects this situation and panics, but there may be more graceful way to interrupt the program and either adjust the transaction IDs on the live record versions or wait until all extant transactions complete before resuming doling out these much lower IDs.

- The HTTP server does not watch for changes to the file storing the X.509 serving certificate and reload it when it changes. If the certificate is due to expire and we issue a replacement, we have to stop and restart the server program to allow it to use the new certificate. We could integrate the :library:`controller-runtime` library's :package:`certwatcher` `package <https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/certwatcher>`__ to address this need.
