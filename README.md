Starting a cluster:

On host A:

    arangodb

will use port 8529 to wait for colleagues. On host B: (can be same as A):
Will fire up an agent on A:4001, but not yet a coordinator or dbserver.
Will persist cluster setup to a file in 

    arangodb A

will contact A:8529 and register, will send its address B and learn
port offset 1 if A=B, or 0 if A!=B. On host C: (can be same as A or B):
Will fire up an agent on B:4001+offset.

    arangodb A

will contact A:8529 and register, will send its address C and learn
port offset for C=A or C=B, or 0 if C is new. Will fire up an agent on
C:4001+offset.

From the moment on when 3 have joined, each will fire up a coordinator and
a dbserver

Options: 

    -directory <place for datadirectories>
     Default: .
    -port <port>
     Default: 8529
    -executable <path>
     Default: /usr/sbin/arangod, depending on OS
    -library <lib>
     Default: /usr/share/arangodb
    -configuration <conf>
     Default: /etc/arangodb3/arangod.conf
    -log <logdir>
     Default: ./ArangoDBData/arangodb.log
    -agencySize <size>
     Default: 3, Servers start as soon as agency is ready
    -coordinator <bool>
     Default: true
    -dbserver <bool>
     Default: true

 
