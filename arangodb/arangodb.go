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
	agencySizeDefault int = 3
	portDefault int       = 4000
	workDirDefault string = "./"
)

var agencySize int
var port int
var workDir string

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
		header.Add("Location", "http://" + myPeers.Hosts[0] + ":" +
		                       strconv.Itoa(port) + "/hello")
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
	if state == STATE_MASTER && r.Method == "POST" {
		var newPeer Peers
		body, _ := ioutil.ReadAll(r.Body)
		r.Body.Close()
		fmt.Println("Received body:", string(body))
		json.Unmarshal(body, &newPeer)
		peerDir := newPeer.Directories[0]

		newGuy := findHost(r.RemoteAddr)
		found := false
		fmt.Println("Guck:", myPeers, peerDir, newGuy)
		for i := len(myPeers.Hosts) - 1; i >= 0; i-- {
			if myPeers.Hosts[i] == newGuy {
				if myPeers.Directories[i] == peerDir {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, `{"error": "Cannot use same directory as peer."}`)
					return
				}
				myPeers.PortOffsets = append(myPeers.PortOffsets,
					myPeers.PortOffsets[i] + 1)
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
	// Basically keep subprocesses running
	for {
		fmt.Println("Making sure that services run...")
		time.Sleep(1000000000)
		if stop {
			break
		}
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
	fmt.Println("Contacting master...")
	b, _ := json.Marshal(Peers{Directories: []string{workDir}})
	buf := bytes.Buffer{}
	buf.Write(b)
	r, e := http.Post("http://" + peerAddress + ":" + strconv.Itoa(port) +
		"/hello", "application/json", &buf)
	if e != nil || r.StatusCode != http.StatusOK {
		fmt.Println("Cannot start because of error from master:", e, r.StatusCode)
		return
	}
	body, _ := ioutil.ReadAll(r.Body)
	r.Body.Close()
	fmt.Println("Body:", string(body), e)
	e = json.Unmarshal(body, &myPeers)
	if e != nil {
		fmt.Println("Cannot parse body from master:", e)
		return
	}
	myPeers.MyIndex = len(myPeers.Hosts) - 1
	agencySize = myPeers.AgencySize

	// Wait until we can start:
	for {
		if len(myPeers.Hosts) >= agencySize {
			fmt.Println("Starting running service...")
			saveSetup()
			state = STATE_RUNNING
			startRunning()
			return
		}
		fmt.Println("Waiting for enough servers to show up...")
		time.Sleep(1000000000)
		r, e = http.Get("http://" + myPeers.Hosts[0] + ":" + strconv.Itoa(port) +
			"/hello")
		body, e = ioutil.ReadAll(r.Body)
		r.Body.Close()
		fmt.Println("Body2:", string(body), e)
    var newPeers Peers
		json.Unmarshal(body, &newPeers)
		myPeers.Hosts = newPeers.Hosts
		myPeers.PortOffsets = newPeers.PortOffsets
	}
}

func startMaster() {
	// Permanent loop:
	for {
		fmt.Println("Serving as master, number of peers:", len(myPeers.Hosts))
		time.Sleep(1000000000)
		select {
	  case <- starter:
			saveSetup()
			fmt.Println("Starting running service...")
			state = STATE_RUNNING
			startRunning()
			return
		default:
			fmt.Println("Nothing received from channel.")
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
