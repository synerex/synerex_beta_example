package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	proto "github.com/golang/protobuf/proto"

	api "github.com/synerex/synerex_api"
	order "github.com/synerex/synerex_beta_example/proto_order"
	sxutil "github.com/synerex/synerex_sxutil"

	"github.com/gorilla/websocket"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var (
	nodesrv         = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	localsx         = flag.String("local", "", "Local Synerex Server")
	mu              sync.Mutex
	tracer          trace.Tracer
	sxServerAddress string
)

const orderChannel uint32 = 1 // just for private
var lastMyOrder uint64

// order supply from Services.
func supplyOrderCallback(clt *sxutil.SXServiceClient, sp *api.Supply) {
	od := &order.Order{}
	dt := sp.Cdata.Entity
	err := proto.Unmarshal(dt, od)
	if err == nil {
		log.Printf("got order! %s %#v", sp.SupplyName, *od)
		if sp.SupplyName == "SupplyOrder" { // check the order for me!
			if sp.TargetId == lastMyOrder { // this is supply for my order
				// create new span! from supply.
				spanCtx := sxutil.UnmarshalSpanContextJSON(sp.ArgJson)
				log.Printf("Get Context %s", sp.ArgJson)
				//  use ctx to next..
				_, span := tracer.Start(
					trace.ContextWithRemoteSpanContext(context.Background(), spanCtx),
					"receive:SupplyOrder",
					trace.WithSpanKind(trace.SpanKindClient),
				)
				// list up orders for selection.
				fmt.Printf("Get %d Items!\n", len(od.Items))
				for i := range od.Items {
					fmt.Printf("%d: %s, price:%d\n", i, od.Items[i].Name, od.Items[i].Price)
				}
				// send order information to WebSocketClients.

				// is OK to end span?
				span.End()
			}
		}
	}
}

func subscribeOrderSupply(client *sxutil.SXServiceClient) {
	for {
		ctx := context.Background() //
		err := client.SubscribeSupply(ctx, supplyOrderCallback)
		log.Printf("Error:Supply %s\n", err.Error())

		time.Sleep(5 * time.Second) // wait 5 seconds to reconnect
		newClt := sxutil.GrpcConnectServer(sxServerAddress)
		if newClt != nil {
			log.Printf("Reconnect server [%s]\n", sxServerAddress)
			client.SXClient = newClt
		}
	}
}

func sendDemand(client *sxutil.SXServiceClient) {
	// order food and beverage

	itf := &order.Item{
		Id:   0,
		Type: order.ItemType_FOOD,
	}
	itb := &order.Item{
		Id:   1,
		Type: order.ItemType_BEVERAGE,
	}

	ord := order.Order{
		OrderId: 0,
		Items:   []*order.Item{itf, itb},
	}
	et, _ := proto.Marshal(&ord)

	// start Trace!
	ctx := context.Background()
	var span trace.Span
	ctx, span = tracer.Start(
		ctx,
		"notifyDemand:OrderDemand",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	log.Println("Context pln", ctx)
	log.Printf("Context  %#v", ctx)
	log.Printf("SpanFromContext  %#v", trace.SpanFromContext(ctx))
	log.Printf("BaseSpan         %#v", span)
	log.Printf("Context With Span %#v", trace.ContextWithSpan(ctx, span))
	sc := span.SpanContext()
	js, _ := sc.MarshalJSON()
	log.Printf("MarshalJSON of SpanContext %s", string(js))

	opts := &sxutil.DemandOpts{
		Name: "OrderDemand",
		JSON: string(js),
		Cdata: &api.Content{
			Entity: et,
		},
		Context: ctx,
	}
	span.AddEvent("beforeSending")

	lastMyOrder, _ = client.NotifyDemand(opts)
	span.AddEvent("afterSending")
	log.Printf("Send Demand %#v", opts)
	log.Printf("SpanEnd! %v", span)
	log.Printf("SpanEnd! %#v", span)
	span.End()

}

/* Websocket clients */

type WClient struct {
	ws   *WSServ
	conn *websocket.Conn
	send chan []byte
}

type WSServ struct {
	clients    map[*WClient]bool // connected cilents
	register   chan *WClient
	unregister chan *WClient
	broadcast  chan []byte
}

func NewWSServ() *WSServ {
	return &WSServ{
		broadcast:  make(chan []byte),
		clients:    make(map[*WClient]bool),
		register:   make(chan *WClient),
		unregister: make(chan *WClient),
	}
}

// main goroutine for Terminal server
func (ws *WSServ) run() {

	for {
		select {
		case client := <-ws.register:
			ws.clients[client] = true
		case client := <-ws.unregister:
			if _, ok := ws.clients[client]; ok {
				delete(ws.clients, client)
				close(client.send)
			}
		case message := <-ws.broadcast: // broadcasting to all clients.
			for client := range ws.clients {
				select { // non-blocking send
				case client.send <- message:
				default:
					close(client.send)
					delete(ws.clients, client)
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

func (ts *WSServ) messageHandler(msg []byte) {
	// handling message from client
	str := string(msg)
	log.Printf("get message[%s]", str)

}

// read client message each terminals.
func (c *WClient) readPump() {
	defer func() {
		c.ws.unregister <- c
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
		go c.ws.messageHandler(message)
	}
}
func (c *WClient) writePump() {
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func servWS(ws *WSServ, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &WClient{ws: ws, conn: conn, send: make(chan []byte, 256)}
	ws.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

func main() {
	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)
	log.Printf("User Provider(%s) built %s sha1 %s", sxutil.GitVer, sxutil.BuildTime, sxutil.Sha1Ver)

	channelTypes := []uint32{orderChannel} // use sample channel type

	var rerr error
	sxServerAddress, rerr = sxutil.RegisterNode(*nodesrv, "UserProvider", channelTypes, nil)

	tp := sxutil.NewOltpTracer() // this enables default tracer!
	defer func() {
		log.Printf("Closing oltp traceer")
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Can't shoutdown tracer %v", err)
		}
	}()
	tracer = otel.Tracer("user_provider", trace.WithInstrumentationVersion("0.0.1"))
	log.Printf("Tracer generated %#v", tracer)

	if rerr != nil {
		log.Fatal("Can't register node:", rerr)
	}
	if *localsx != "" { // quick hack for AWS local network
		sxServerAddress = *localsx
	}
	log.Printf("Connecting SynerexServer at [%s]", sxServerAddress)

	wg := sync.WaitGroup{} // for syncing other goroutines

	client := sxutil.GrpcConnectServer(sxServerAddress)

	if client == nil {
		log.Fatal("Can't connect Synerex Server")
	} else {
		log.Print("Connecting SynerexServer")
	}

	geClient := sxutil.NewSXServiceClient(client, orderChannel, "{Client:UserProvider}")

	wg.Add(1)
	log.Print("Subscribe Supply")
	go subscribeOrderSupply(geClient)

	router := gin.Default()
	router.GET("/menu", func(c *gin.Context) {
		log.Printf("Get from %#v", c)
		c.Header("Access-Control-Allow-Origin", "*")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("getMenu"))

		sendDemand(geClient)
	})

	ws := NewWSServ() // web socket server
	// for websocket
	router.GET("/ws", func(c *gin.Context) {
		servWS(ws, c.Writer, c.Request)
	})

	router.Run(":8081")

	wg.Wait()

}
