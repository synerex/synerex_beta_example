package main

import (
	"bufio"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var (
	jaegerHost = flag.String("jaegerHost", "127.0.0.1", "OpenTelemetry Jaeger host")
)

// terminal service using WebScoket

//
//   serv  <- client (term1, term2, term3...)
//
// client -> "id,cmd"   // run command with terminal ID
// serv   -> "id,txt"   // show reply txt to terminal ID

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// variable for keeping running commands.
var runCmds map[string]*exec.Cmd

// websocket clients
type TClient struct {
	ts   *TServ
	conn *websocket.Conn
	send chan []byte
}

// TServ
type TServ struct {
	clients    map[*TClient]bool
	register   chan *TClient
	unregister chan *TClient
	broadcast  chan []byte
}

func NewTServ() *TServ {
	return &TServ{
		broadcast:  make(chan []byte),
		clients:    make(map[*TClient]bool),
		register:   make(chan *TClient),
		unregister: make(chan *TClient),
	}
}

// main goroutine for Terminal server
func (ts *TServ) run() {
	// initialize cmds
	runCmds = make(map[string]*exec.Cmd)

	for {
		select {
		case client := <-ts.register:
			ts.clients[client] = true
		case client := <-ts.unregister:
			if _, ok := ts.clients[client]; ok {
				delete(ts.clients, client)
				close(client.send)
			}
		case message := <-ts.broadcast: // broadcasting to all clients.
			for client := range ts.clients {
				select { // non-blocking send
				case client.send <- message:
				default:
					close(client.send)
					delete(ts.clients, client)
				}
			}
		}
	}
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

// read client message each terminals.
func (c *TClient) readPump() {
	defer func() {
		c.ts.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("TS websocket error: %v", err)
			}
			break
		}
		go c.ts.messageHandler(message)
	}
}
func (c *TClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send) //
			for i := 0; i < n; i++ {
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func ServeTServWs(ts *TServ, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &TClient{ts: ts, conn: conn, send: make(chan []byte, 256)}
	ts.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

func (ts *TServ) sendTerm(id string, msg string) {
	cmd := id + "," + msg
	bmsg := []byte(cmd)

	// broadcast to all clients
	//	log.Printf("sending %s:%s", id, cmd)
	log.Printf("%s", cmd)
	ts.broadcast <- bmsg

}

func (ts *TServ) messageHandler(msg []byte) {
	// handling message from client
	str := string(msg)
	log.Printf("get message[%s]", str)
	v := strings.SplitN(str, ",", 2)
	id := v[0]
	cmd := v[1]
	go ts.runCommand(id, cmd) // starting new command with Terminai ID
}

// exec local command connected with remote terminal
func (ts *TServ) runCommand(termId string, cmdStr string) {
	pcmd, ok := runCmds[cmdStr]
	if ok {
		log.Printf("checking current status of %s", cmdStr)
		// we should stop the pcmd, so check the state
		log.Printf("cmd process %#v %#v %#v", pcmd, pcmd.ProcessState, pcmd.Process)
		if pcmd.ProcessState == nil { // still running
			log.Printf("killing process %#v", pcmd)
			pcmd.Process.Signal(syscall.SIGINT)
			time.Sleep(100 * time.Millisecond) // wait to close old channels
			pcmd.Process.Kill()
			pcmd.Process.Wait() // wait until the process stops.
			log.Printf("killed process %#v", pcmd)
			ts.sendTerm(termId, "process "+cmdStr+" stopped!")

			/*			if pcmd.ProcessState.Exited() {
							log.Printf("Process %s stopped and restart", cmdStr)
							ts.sendTerm(termId, "process "+cmdStr+" restart!")
							time.Sleep(100 * time.Millisecond) // wait to close old channels
						} else {
							log.Printf("Can't stop the process %#v %s", pcmd, pcmd.ProcessState.String())
						}
			*/

		}
		delete(runCmds, cmdStr)
		//toggle run/start
		return
	}
	// if cmdStr contains space, we should split them.
	args := strings.Fields(cmdStr)
	cmd := exec.Command(args[0], args[1:]...)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	// we need to setup environment variable.
	cmd.Env = append(os.Environ(),
		"OTEL_EXPORTER_JAEGER_AGENT_HOST="+*jaegerHost,
		"OTEL_EXPORTER_JAEGER_ENDPOINT=http://"+*jaegerHost+":14268/api/traces",
		"OTEL_SERVICE_NAME="+termId,
	)
	err := cmd.Start() //
	if err != nil {
		log.Printf("Cmd %s start Error:%v", cmdStr, err)
		return // do not stream..
	} else {
		log.Printf("%s:Cmd %s starts! with env %#v", termId, cmdStr, cmd.Env)
		runCmds[cmdStr] = cmd
	}

	outChan := make(chan string)
	doneChan := make(chan bool)
	defer close(outChan)
	defer close(doneChan)
	loop := true

	streamReader := func(st string, r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if loop {
				outChan <- st + scanner.Text()
			} else {
				break
			}
		}
		if loop {
			select {
			case doneChan <- true:
			default:
				log.Printf("done closed.")
			}
		}
	}
	go streamReader("", stdout)
	go streamReader("", stderr)

	for loop {
		select {
		case <-doneChan:
			loop = false
		case line := <-outChan:
			ts.sendTerm(termId, line)
		}
	}

}
