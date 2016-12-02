package main 

import (
	"encoding/json"
	"fmt"
	"io"
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

// State of peers:

type Peers struct {
	Hosts []string
  PortOffsets []int
}

var myPeers Peers

// A helper function:

func findHost(a string) string {
	pos := strings.Index(a, ":")
	if pos > 0 {
		return a[:pos]
	}
	return a
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
	for i := len(myPeers.Hosts)-2; i >= 0; i-- {
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
	// Interrupt signal:
	sigChannel = make(chan os.Signal)
	signal.Notify(sigChannel, os.Interrupt)
	go handleSignal()

	// HTTP service:
	http.HandleFunc("/hello", hello)
	go http.ListenAndServe("0.0.0.0:" + strconv.Itoa(port), nil)

	// Do we have to register?
	if len(os.Args) > 1 {
    // The argument is the address of a host
		fmt.Println("Contacting peers...")
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