Starting an ArangoDB cluster the easy way
=========================================

Building
--------

Just do

    cd arangodb
    go build

and the executable is in `arangodb/arangodb`. You can install it
anywhere in your path. This program will run on Linux, OSX or Windows.

Starting a cluster
------------------

Install ArangoDB in the usual way as binary package. Then:

On host A:

    arangodb

This will use port 4000 to wait for colleagues (3 are needed for a
resilient agency). On host B: (can be the same as A):

    arangodb --join A

This will contact A:4000 and register. On host C: (can be same as A or B):

    arangodb --join A

will contact A:4000 and register.

From the moment on when 3 have joined, each will fire up an agent, a 
coordinator and a dbserver and the cluster is up. Ports are shown on
the console.

Additional servers can be added in the same way.

If two or more of the `arangodb` instances run on the same machine,
one has to use the `--dataDir` option to let each use a different
directory.

The `arangodb` program will find the ArangoDB executable and the
other installation files automatically. If this fails, use the
`--arangod` and `--jsdir` options described below.

Common options 
--------------

* `--dataDir path`
        directory to store all data (default "./")
* `--join addr`
        join a cluster with master at address addr (default "")
* `--agencySize int`
        number of agents in agency (default 3)
* `--ownAddress addr`
        address under which this server is reachable, needed for 
        the case of `--agencySize 1` in the master

Esoteric options
----------------

* `--masterPort int`
        port for arangodb master (default 4000)
* `--arangod path`
        path to arangod executable (default "/usr/sbin/arangod")
* `--jsDir path`
        path to JS library directory (default "/usr/share/arangodb3/js")
* `--startCoordinator bool`
        should a coordinator instance be started (default true)
* `--startDBserver bool`
        should a dbserver instance be started (default true)
* `--rr path`
        path to rr executable to use if non-empty (default "")
* `--verbose bool`
        show more information (default false)
	
Technical explanation as to what happens
----------------------------------------


