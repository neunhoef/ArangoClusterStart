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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Configuration data with defaults:

var agencySize = 3
var arangodExecutable = "/usr/sbin/arangod"
var arangodJSstartup = "/usr/share/arangodb3/js"
var logLevel = "INFO"
var port = 4000
var rrPath = ""
var startCoordinator = true
var startDBserver = true
var workDir = "./"

// Overall state:

const (
	stateStart   int = iota // initial state after start
	stateMaster  int = iota // finding phase, first instance
	stateSlave   int = iota // finding phase, further instances
	stateRunning int = iota // running phase
)

var state = stateStart
var starter = make(chan bool)

// State of peers:

type peers struct {
	Hosts       []string
	PortOffsets []int
	Directories []string
	MyIndex     int
	AgencySize  int
}

var myPeers peers

// A helper function:

func findHost(a string) string {
	pos := strings.LastIndex(a, ":")
	var host string
	if pos > 0 {
		host = a[:pos]
	} else {
		host = a
	}
	if host == "127.0.0.1" || host == "[::1]" {
		host = "localhost"
	}
	return host
}

// HTTP service function:

func hello(w http.ResponseWriter, r *http.Request) {
	if state == stateSlave {
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
		var newPeer peers
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()
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
		fmt.Println("New peer:", newGuy+", portOffset: "+
			strconv.Itoa(myPeers.PortOffsets[len(myPeers.PortOffsets)-1]))
		if len(myPeers.Hosts) == agencySize {
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

func slasher(s string) string {
	return strings.Replace(s, "\\", "/", -1)
}

var confFileTemplate = `# ArangoDB configuration file
#
# Documentation:
# https://docs.arangodb.com/Manual/Administration/Configuration/
#

[server]
endpoint = tcp://0.0.0.0:%s
threads = %d

[log]
level = %s

[javascript]
v8-contexts = %d
`

func makeBaseArgs(myDir string, myAddress string, myPort string,
	mode string) (args []string) {

	confFileName := myDir + "arangod.conf"
	if _, err := os.Stat(confFileName); os.IsNotExist(err) {
		out, e := os.Create(confFileName)
		if e != nil {
			fmt.Println("Could not create configuration file", confFileName, "error:",
				e)
			os.Exit(1)
		}
		switch mode {
		// Parameters are: port, server threads, log level, v8-contexts
		case "agent":
			fmt.Fprintf(out, confFileTemplate, myPort, 8, logLevel, 1)
		case "dbserver":
			fmt.Fprintf(out, confFileTemplate, myPort, 4, logLevel, 4)
		case "coordinator":
			fmt.Fprintf(out, confFileTemplate, myPort, 16, logLevel, 4)
		}
		out.Close()
	}
	args = make([]string, 0, 40)
	if rrPath != "" {
		args = append(args, rrPath)
	}
	args = append(args,
		arangodExecutable,
		"-c", slasher(confFileName),
		"--database.directory", slasher(myDir+"data"),
		"--javascript.startup-directory", slasher(arangodJSstartup),
		"--javascript.app-path", slasher(myDir+"apps"),
		"--log.file", slasher(myDir+"arangod.log"),
		"--log.force-direct", "false",
		"--server.authentication", "false",
	)
	switch mode {
	case "agent":
		args = append(args,
			"--agency.activate", "true",
			"--agency.my-address", "tcp://"+myAddress+myPort,
			"--agency.size", strconv.Itoa(agencySize),
			"--agency.supervision", "true",
			"--foxx.queues", "false",
			"--server.statistics", "false",
		)
		for i := 0; i < agencySize; i++ {
			if i != myPeers.MyIndex {
				args = append(args,
					"--agency.endpoint",
					"tcp://"+myPeers.Hosts[i]+":"+
						strconv.Itoa(4001+myPeers.PortOffsets[i]))
			}
		}
	case "dbserver":
		args = append(args,
			"--cluster.my-address", "tcp://"+myAddress+myPort,
			"--cluster.my-role", "PRIMARY",
			"--cluster.my-local-info", myAddress+myPort,
			"--foxx.queues", "false",
			"--server.statistics", "true",
		)
	case "coordinator":
		args = append(args,
			"--cluster.my-address", "tcp://"+myAddress+myPort,
			"--cluster.my-role", "COORDINATOR",
			"--cluster.my-local-info", myAddress+myPort,
			"--foxx.queues", "true",
			"--server.statistics", "true",
		)
	}
	if mode != "agent" {
		for i := 0; i < agencySize; i++ {
			args = append(args,
				"--cluster.agency-endpoint",
				"tcp://"+myPeers.Hosts[i]+":"+
					strconv.Itoa(4001+myPeers.PortOffsets[i]))
		}
	}
	return
}

func writeCommand(filename string, executable string, args []string) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		out, err := os.Create(filename)
		if err == nil {
			for _, s := range args {
				fmt.Fprintf(out, " %s", s)
			}
			fmt.Fprintf(out, "\n")
			out.Close()
		}
	}
}

func startRunning() {
	state = stateRunning
	myAddress := myPeers.Hosts[myPeers.MyIndex] + ":"
	portOffset := myPeers.PortOffsets[myPeers.MyIndex]
	var myPort string
	var myDir string
	var args []string

	// Start agent:
	var agentProc *os.Process
	var err error
	var executable string
	if rrPath != "" {
		executable = rrPath
	} else {
		executable = arangodExecutable
	}
	if myPeers.MyIndex < agencySize {
		myPort = strconv.Itoa(4001 + portOffset)
		myDir = workDir + "agent" + myPort + string(os.PathSeparator)
		os.MkdirAll(myDir+"data", 0755)
		os.MkdirAll(myDir+"apps", 0755)
		args = makeBaseArgs(myDir, myAddress, myPort, "agent")
		writeCommand(myDir+"arangod_command.txt", executable, args)
		agentProc, err = os.StartProcess(executable, args,
			&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
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
		args = makeBaseArgs(myDir, myAddress, myPort, "dbserver")
		writeCommand(myDir+"arangod_command.txt", executable, args)
		dbserverProc, err = os.StartProcess(executable, args,
			&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
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
		args = makeBaseArgs(myDir, myAddress, myPort, "coordinator")
		writeCommand(myDir+"arangod_command.txt", executable, args)
		coordinatorProc, err = os.StartProcess(executable, args,
			&os.ProcAttr{"", nil, []*os.File{os.Stdin, nil, nil}, nil})
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
	b, _ := json.Marshal(peers{Directories: []string{workDir}})
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
			startRunning()
			return
		}
		time.Sleep(1000000000)
		r, e = http.Get("http://" + myPeers.Hosts[0] + ":" + strconv.Itoa(port) +
			"/hello")
		body, e = ioutil.ReadAll(r.Body)
		r.Body.Close()
		var newPeers peers
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
			startRunning()
			return
		default:
		}
		if stop {
			break
		}
	}
}

func findExecutable() {
	var pathList = make([]string, 0, 10)
	pathList = append(pathList, "build/bin/arangod")
	switch runtime.GOOS {
	case "windows":
		// Look in the default installation location:
		foundPaths := make([]string, 0, 20)
		basePath := "C:/Program Files"
		d, e := os.Open(basePath)
		if e == nil {
			l, e := d.Readdir(1024)
			if e == nil {
				for _, n := range l {
					if !n.IsDir() {
						name := n.Name()
						fmt.Println("Found name", name)
						if strings.HasPrefix(name, "ArangoDB3 ") {
							foundPaths = append(foundPaths, basePath + "/" + name +
								"/usr/bin/arangod.exe")
							fmt.Println("foundPaths:", foundPaths)
						}
					}
				}
			} else {
				fmt.Println("Could not read directory", basePath,
					"to look for executable.")
			}
			d.Close()
		} else {
			fmt.Println("Could not open directory", basePath,
				"to look for executable.")
		}
		sort.Sort(sort.Reverse(sort.StringSlice(foundPaths)))
		fmt.Println("foundPaths2:", foundPaths)
		pathList = append(pathList, foundPaths...)
	case "darwin":
		pathList = append(pathList,
			"/Applications/ArangoDB3-CLI.app/Contents/MacOS/usr/sbin/arangod",
			"/usr/local/opt/arangodb/sbin/arangod",
		)
	case "linux":
		pathList = append(pathList,
			"/usr/sbin/arangod",
		)
	}
	for _, p := range pathList {
		if _, e := os.Stat(filepath.Clean(filepath.FromSlash(p))); e == nil || !os.IsNotExist(e) {
			arangodExecutable, _ = filepath.Abs(filepath.FromSlash(p))
			if p == "build/bin/arangod" {
				arangodJSstartup, _ = filepath.Abs("js")
			} else {
				arangodJSstartup, _ = filepath.Abs(
					filepath.FromSlash(filepath.Dir(p) + "/../share/arangodb3/js"))
			}
			fmt.Println("Found", arangodExecutable, "as default arangod executable.")
			fmt.Println("Using", arangodJSstartup, "as default JS dir.")
			return
		}
	}
}

func main() {
	// Find executable and jsdir default in a platform dependent way:
	findExecutable()

	// Command line arguments:
	flag.IntVar(&agencySize, "agencySize", agencySize,
		"number of agents in agency")
	flag.IntVar(&port, "port", port, "port for arangodb launcher")
	flag.StringVar(&workDir, "workDir", workDir, "working directory")
	flag.StringVar(&arangodExecutable, "arangod", arangodExecutable,
		"path to arangod executable")
	flag.StringVar(&arangodJSstartup, "jsdir", arangodJSstartup,
		"path to JS library directory")
	flag.BoolVar(&startCoordinator, "coordinator", startCoordinator,
		"start a coordinator instance")
	flag.BoolVar(&startDBserver, "dbserver", startDBserver,
		"start a dbserver instance")
	flag.StringVar(&rrPath, "rr", rrPath, "path to rr executable to use")
	flag.StringVar(&logLevel, "loglevel", logLevel,
		"log level (ERROR, INFO, DEBUG, TRACE)")
	flag.Parse()

	// Sort out work directory:
	if len(workDir) == 0 {
		workDir = "./"
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
				fmt.Println("Relaunching service...")
				startRunning()
				return
			}
		}
	}

	// Do we have to register?
	args := flag.Args()
	if len(args) > 0 {
		state = stateSlave
		startSlave(args[0])
	} else {
		state = stateMaster
		startMaster()
	}
}
