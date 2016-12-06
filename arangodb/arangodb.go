package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Configuration data:

const (
	agencySizeDefault        int    = 3
	portDefault              int    = 4000
	workDirDefault           string = "./"
	arangodExecutableDefault string = "/usr/sbin/arangod"
	arangodJSstartupDefault  string = "/usr/share/arangodb3/js"
	startCoordinatorDefault  bool   = true
	startDBserverDefault     bool   = true
	rrPathDefault            string = ""
)

var agencySize int
var port int
var workDir string
var arangodExecutable string
var arangodJSstartup string
var startCoordinator bool
var startDBserver bool
var rrPath string

// Overall state:

const (
	STATE_START   int = iota // initial state after start
	STATE_MASTER  int = iota // finding phase, first instance
	STATE_SLAVE   int = iota // finding phase, further instances
	STATE_RUNNING int = iota // running phase
)

var state int = STATE_START
var starter chan bool = make(chan bool)

// State of peers:

type Peers struct {
	Hosts       []string
	PortOffsets []int
	Directories []string
	MyIndex     int
	AgencySize  int
}

var myPeers Peers

// A helper function:

func findHost(a string) string {
	pos := strings.Index(a, ":")
	var host string
	if pos > 0 {
		host = a[:pos]
	} else {
		host = a
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return host
}

// HTTP service function:

func hello(w http.ResponseWriter, r *http.Request) {
	if state == STATE_SLAVE {
		header := w.Header()
		header.Add("Location", "http://"+myPeers.Hosts[0]+":"+
			strconv.Itoa(port)+"/hello")
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	}
	if len(myPeers.Hosts) == 0 {
		myself := findHost(r.Host)
		myPeers.Hosts = append(myPeers.Hosts, myself)
		myPeers.PortOffsets = append(myPeers.PortOffsets, 0)
		myPeers.Directories = append(myPeers.Directories, workDir)
		myPeers.AgencySize = agencySize
		myPeers.MyIndex = 0
	}
	if r.Method == "POST" {
		var newPeer Peers
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()
		//fmt.Println("Received body:", string(body))
		json.Unmarshal(body, &newPeer)
		peerDir := newPeer.Directories[0]

		newGuy := findHost(r.RemoteAddr)
		found := false
		for i := len(myPeers.Hosts) - 1; i >= 0; i-- {
			if myPeers.Hosts[i] == newGuy {
				if myPeers.Directories[i] == peerDir {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, `{"error": "Cannot use same directory as peer."}`)
					return
				}
				myPeers.PortOffsets = append(myPeers.PortOffsets,
					myPeers.PortOffsets[i]+1)
				myPeers.Directories = append(myPeers.Directories, peerDir)
				found = true
				break
			}
		}
		myPeers.Hosts = append(myPeers.Hosts, newGuy)
		if !found {
			myPeers.PortOffsets = append(myPeers.PortOffsets, 0)
			myPeers.Directories = append(myPeers.Directories, newPeer.Directories[0])
		}
		fmt.Println("New peers:", myPeers)
		if len(myPeers.Hosts) >= agencySize {
			starter <- true
		}
	}
	b, e := json.Marshal(myPeers)
	if e != nil {
		io.WriteString(w, "Hello world! Your address is:"+r.RemoteAddr)
	} else {
		w.Write(b)
	}
}

// Stuff for the signal handling:

var sigChannel chan os.Signal
var stop = false

func handleSignal() {
	for s := range sigChannel {
		fmt.Println("Received signal:", s)
		stop = true
	}
}

func startRunning() {
	myAddress := myPeers.Hosts[myPeers.MyIndex] + ":"
	portOffset := myPeers.PortOffsets[myPeers.MyIndex]
	var myPort string
	var myDir string
	var args []string

	// Start agent:
	var agentProc *os.Process
	var err error
	if myPeers.MyIndex < agencySize {
		myPort = strconv.Itoa(4001 + portOffset)
		myDir = workDir + "agent" + myPort + string(os.PathSeparator)
		os.MkdirAll(myDir+"data", 0755)
		os.MkdirAll(myDir+"apps", 0755)
		args = make([]string, 0, 30)
		if rrPath != "" {
			args = append(args, rrPath)
		}
		args = append(args,
			arangodExecutable,
			"-c", "none",
			"--agency.activate", "true",
			"--server.endpoint", "tcp://0.0.0.0:"+myPort,
			"--agency.my-address", "tcp://"+myAddress+myPort,
			"--agency.size", strconv.Itoa(agencySize),
			"--agency.supervision", "true",
			"--database.directory", myDir+"data",
			"--foxx.queues", "false",
			"--javascript.startup-directory", arangodJSstartup,
			"--javascript.app-path", myDir+"apps",
			//"--javascript.v8-contexts", "1",
			"--server.statistics", "false",
			"--server.threads", "16",
			"--log.file", myDir+"arangod.log",
			"--log.level=agency=debug",
			"--log.force-direct", "true",
			"--server.authentication", "false")
		for i := 0; i < agencySize; i++ {
			if i != myPeers.MyIndex {
				args = append(args,
					"--agency.endpoint",
					"tcp://"+myPeers.Hosts[i]+":"+
						strconv.Itoa(4001+myPeers.PortOffsets[i]))
			}
		}
		if rrPath != "" {
			agentProc, err = os.StartProcess(rrPath, args,
				&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
		} else {
			agentProc, err = os.StartProcess(arangodExecutable, args,
				&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
		}
		if err != nil {
			fmt.Println("Error whilst starting agent:", err)
		}
	}

	// Start DBserver:
	var dbserverProc *os.Process
	if startDBserver {
		myPort = strconv.Itoa(8629 + portOffset)
		myDir = workDir + "dbserver" + myPort + string(os.PathSeparator)
		os.MkdirAll(myDir+"data", 0755)
		os.MkdirAll(myDir+"apps", 0755)
		args = make([]string, 0, 30)
		args = append(args,
			arangodExecutable,
			"-c", "none",
			"--database.directory", myDir+"data",
			"--server.endpoint", "tcp://0.0.0.0:"+myPort,
			"--cluster.my-address", "tcp://"+myAddress+myPort,
			"--cluster.my-role", "PRIMARY",
			"--cluster.my-local-info", myAddress+myPort,
			"--foxx.queues", "false",
			"--javascript.startup-directory", arangodJSstartup,
			"--javascript.app-path", myDir+"apps",
			"--server.statistics", "true",
			"--server.threads", "8",
			"--log.file", myDir+"arangod.log",
			"--server.authentication", "false")
		for i := 0; i < agencySize; i++ {
			args = append(args,
				"--cluster.agency-endpoint",
				"tcp://"+myPeers.Hosts[i]+":"+
					strconv.Itoa(4001+myPeers.PortOffsets[i]))
		}
		if rrPath != "" {
			dbserverProc, err = os.StartProcess(rrPath, args,
				&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
		} else {
			dbserverProc, err = os.StartProcess(arangodExecutable, args,
				&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
		}
		if err != nil {
			fmt.Println("Error whilst starting dbserver:", err)
		}
	}

	// Start Coordinator:
	var coordinatorProc *os.Process
	if startCoordinator {
		myPort = strconv.Itoa(8530 + portOffset)
		myDir = workDir + "coordinator" + myPort + string(os.PathSeparator)
		os.MkdirAll(myDir+"data", 0755)
		os.MkdirAll(myDir+"apps", 0755)
		args = make([]string, 0, 30)
		args = append(args,
			arangodExecutable,
			"-c", "none",
			"--database.directory", myDir+"data",
			"--server.endpoint", "tcp://0.0.0.0:"+myPort,
			"--cluster.my-address", "tcp://"+myAddress+myPort,
			"--cluster.my-role", "COORDINATOR",
			"--cluster.my-local-info", myAddress+myPort,
			"--foxx.queues", "true",
			"--javascript.startup-directory", arangodJSstartup,
			"--javascript.app-path", myDir+"apps",
			"--server.statistics", "true",
			"--server.threads", "16",
			"--log.file", myDir+"arangod.log",
			"--server.authentication", "false")
		for i := 0; i < agencySize; i++ {
			args = append(args,
				"--cluster.agency-endpoint",
				"tcp://"+myPeers.Hosts[i]+":"+
					strconv.Itoa(4001+myPeers.PortOffsets[i]))
		}
		if rrPath != "" {
			coordinatorProc, err = os.StartProcess(rrPath, args,
				&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
		} else {
			coordinatorProc, err = os.StartProcess(arangodExecutable, args,
				&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
		}
		if err != nil {
			fmt.Println("Error whilst starting coordinator:", err)
		}
	}

	for {
		time.Sleep(1000000000)
		if stop {
			break
		}
	}

	fmt.Println("Shutting down services...")
	if coordinatorProc != nil {
		coordinatorProc.Kill()
	}
	if dbserverProc != nil {
		dbserverProc.Kill()
	}
	time.Sleep(3000000000)
	if agentProc != nil {
		agentProc.Kill()
	}
}

func saveSetup() {
	f, e := os.Create(workDir + "setup.json")
	defer f.Close()
	if e != nil {
		fmt.Println("Error writing setup:", e)
		return
	}
	b, e := json.Marshal(myPeers)
	if e != nil {
		fmt.Println("Cannot serialize myPeers:", e)
		return
	}
	f.Write(b)
}

func startSlave(peerAddress string) {
	fmt.Println("Contacting master", peerAddress, "...")
	b, _ := json.Marshal(Peers{Directories: []string{workDir}})
	buf := bytes.Buffer{}
	buf.Write(b)
	r, e := http.Post("http://"+peerAddress+":"+strconv.Itoa(port)+
		"/hello", "application/json", &buf)
	if e != nil || r.StatusCode != http.StatusOK {
		fmt.Println("Cannot start because of error from master:", e, r.StatusCode)
		return
	}
	body, _ := ioutil.ReadAll(r.Body)
	r.Body.Close()
	//fmt.Println("Body:", string(body), e)
	e = json.Unmarshal(body, &myPeers)
	if e != nil {
		fmt.Println("Cannot parse body from master:", e)
		return
	}
	myPeers.MyIndex = len(myPeers.Hosts) - 1
	agencySize = myPeers.AgencySize

	// Wait until we can start:
	fmt.Println("Waiting for enough servers to show up...")
	for {
		if len(myPeers.Hosts) >= agencySize {
			fmt.Println("Starting running service...")
			saveSetup()
			state = STATE_RUNNING
			startRunning()
			return
		}
		time.Sleep(1000000000)
		r, e = http.Get("http://" + myPeers.Hosts[0] + ":" + strconv.Itoa(port) +
			"/hello")
		body, e = ioutil.ReadAll(r.Body)
		r.Body.Close()
		//fmt.Println("Body2:", string(body), e)
		var newPeers Peers
		json.Unmarshal(body, &newPeers)
		myPeers.Hosts = newPeers.Hosts
		myPeers.PortOffsets = newPeers.PortOffsets
	}
}

func startMaster() {
	// Permanent loop:
	fmt.Println("Serving as master...")
	for {
		time.Sleep(1000000000)
		select {
		case <-starter:
			saveSetup()
			fmt.Println("Starting running service...")
			state = STATE_RUNNING
			startRunning()
			return
		default:
		}
		if stop {
			break
		}
	}
}

func main() {
	// Command line arguments:
	flag.IntVar(&agencySize, "agencySize", agencySizeDefault,
		"number of agents in agency")
	flag.IntVar(&port, "port", portDefault, "port for arangodb launcher")
	flag.StringVar(&workDir, "workDir", workDirDefault, "working directory")
	flag.StringVar(&arangodExecutable, "arangod", arangodExecutableDefault,
		"path to arangod executable")
	flag.StringVar(&arangodJSstartup, "jsdir", arangodJSstartupDefault,
		"path to JS library directory")
	flag.BoolVar(&startCoordinator, "coordinator", startCoordinatorDefault,
		"start a coordinator instance")
	flag.BoolVar(&startDBserver, "dbserver", startDBserverDefault,
		"start a dbserver instance")
	flag.StringVar(&rrPath, "rr", rrPathDefault, "path to rr executable to use")
	flag.Parse()

	// Sort out work directory:
	if len(workDir) == 0 {
		workDir = workDirDefault
	}
	workDir, _ = filepath.Abs(workDir)
	if workDir[len(workDir)-1] != os.PathSeparator {
		workDir = workDir + string(os.PathSeparator)
	}
	err := os.MkdirAll(workDir, 0755)
	if err != nil {
		fmt.Println("Cannot create working directory", workDir, ", giving up.")
		return
	}

	// Interrupt signal:
	sigChannel = make(chan os.Signal)
	signal.Notify(sigChannel, os.Interrupt)
	go handleSignal()

	// HTTP service:
	http.HandleFunc("/hello", hello)
	go http.ListenAndServe("0.0.0.0:"+strconv.Itoa(port), nil)

	// Is this a new start or a restart?
	setupFile, err := os.Open(workDir + "setup.json")
	if err == nil {
		// Could read file
		setup, err := ioutil.ReadAll(setupFile)
		setupFile.Close()
		if err == nil {
			err = json.Unmarshal(setup, &myPeers)
			if err == nil {
				state = STATE_RUNNING
				startRunning()
				return
			}
		}
	}

	// Do we have to register?
	args := flag.Args()
	if len(args) > 0 {
		state = STATE_SLAVE
		startSlave(args[0])
	} else {
		state = STATE_MASTER
		startMaster()
	}
}
