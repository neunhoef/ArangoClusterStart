package main 

import (
	// "bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"strconv"
	"time"
)

// Configuration data:

var agencySize int = 3
var port int = 8529
var workDir string = "./ArangoDBdata/"

// State of peers:

type Peers struct {
	Hosts []string
  PortOffsets []int
	AgencySize int
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
	if len(myPeers.Hosts) == 0 {
    myself := findHost(r.Host)
		myPeers.Hosts = append(myPeers.Hosts, myself)
	  myPeers.PortOffsets = append(myPeers.PortOffsets, 0)
	}
	newGuy := findHost(r.RemoteAddr)
  myPeers.Hosts = append(myPeers.Hosts, newGuy)
	found := false
	for i := len(myPeers.Hosts) - 2; i >= 0; i-- {
		if myPeers.Hosts[i] == newGuy {
			myPeers.PortOffsets = append(myPeers.PortOffsets,
			                             myPeers.PortOffsets[i] + 1)
      found = true
			break
		}
	}
	if !found {
		myPeers.PortOffsets = append(myPeers.PortOffsets, 0)
	}
	fmt.Println("Peers:", myPeers)
	b, e := json.Marshal(myPeers)
	if e != nil {
		io.WriteString(w, "Hello world! Your address is:" + r.RemoteAddr)
	} else {
		w.Write(b)
	}
}

// Launching an agent:

var agentLaunch bool = false

func agentLauncher() {
	fmt.Println("Launching agent and coordinator and dbserver...")
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

func main() {
	// Command line arguments:
	flag.IntVar(&agencySize, "agencySize", 3, "number of agents in agency")
  flag.IntVar(&port, "port", 8529, "port for arangodb launcher")
	flag.StringVar(&workDir, "workDir", "./ArangoDBdata/", "working directory")
	flag.Parse()

	// Sort out work directory:
	if len(workDir) == 0 {
		workDir = "./ArangoDBdata/"
	}
	if workDir[len(workDir)-1] != '/' {
		workDir = workDir + "/"
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
	go http.ListenAndServe("0.0.0.0:" + strconv.Itoa(port), nil)

	// Do we have to register?
	args := flag.Args()
	if len(args) > 0 {
    // The argument is the address of a host
		fmt.Println("Contacting peers...")
		peerAddress := args[0]
		r, e := http.Get("http://" + peerAddress + ":" + strconv.Itoa(port) +
	                   "/hello")
		fmt.Println(r, e)
		defer r.Body.Close()
		body, e := ioutil.ReadAll(r.Body)
		fmt.Println("Body:", string(body), e)
		json.Unmarshal(body, &myPeers)
	}

	// Permanent loop:
	for {
		fmt.Println("Alive")
		time.Sleep(1000000000)
		if !agentLaunch && len(myPeers.Hosts) >= agencySize {
			go agentLauncher()
			agentLaunch = true
		}
		if stop {
			break
		}
	}
}
