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

will use port 4000 to wait for colleagues. On host B: (can be same as A):

    arangodb A

will contact A:8529 and register, will send its address B and learn
port offset 1 if A=B, or 0 if A!=B. On host C: (can be same as A or B):

    arangodb A

will contact A:8529 and register, will send its address C and learn
port offset for C=A or C=B, or 0 if C is new.

From the moment on when 3 have joined, each will fire up an agent, a 
coordinator and a dbserver and the cluster is up.

Additional servers can be added in the same way.

If two or more of the `arangodb` instances run on the same machine,
one has to use the `-workDir` option to let each use a different
directory.

The `arangodb` program will find the ArangoDB executable and the
other installation files automatically. If this fails, use the
`-arangod` and `-jsdir` options described below.

Options 
-------

    -workDir <place for datadirectories>
     Default: .
    -agencySize <size>
     Default: 3, Servers start as soon as agency is ready
    -arangod <path>
     Default: /usr/sbin/arangod, depending on OS
    -jsdir <lib>
     Default: /usr/share/arangodb3/js
    -coordinator=<bool>
     Default: true, start a coordinator
    -dbserver=<bool>
     Default: true, start a dbserver
    -port <port>
     Default: 4000
    -loglevel <level>
     Default: INFO, other possible values: ERROR, DEBUG, TRACE
    -rr <path-to-rr>
     Default: "", if non-empty, use rr to be found in this path

 
