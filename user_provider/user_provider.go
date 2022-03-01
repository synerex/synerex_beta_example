package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	proto "google.golang.org/protobuf/proto"

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
	tempdir         = flag.String("templateDir", "", "Template Directory")
	tracer          trace.Tracer
	sxServerAddress string
	mainWs          *WSServ
	geClient        *sxutil.SXServiceClient
)

const orderChannel uint32 = 1 // just for private
var lastMyOrder uint64        // for keep last "NofityDemand" id.

// map between "NotifyDemand" and its reply("ProposeSupply") orders
var orderMap map[uint64][]*order.Order = make(map[uint64][]*order.Order)
var supplyMap map[uint64][]*api.Supply = make(map[uint64][]*api.Supply)

// order supply from Services.
func supplyOrderCallback(clt *sxutil.SXServiceClient, sp *api.Supply) {
	od := &order.Order{}
	dt := sp.Cdata.Entity
	//	log.Printf("Got Supply #%v", sp)
	err := proto.Unmarshal(dt, od)
	if err == nil {
		log.Printf("got order! %s %s", sp.SupplyName, od)
		log.Printf(" orderSender %d", sp.SenderId)
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
				//				fmt.Printf("Get %d Items!\n", len(od.Items))
				orderMap[lastMyOrder] = append(orderMap[lastMyOrder], od)
				supplyMap[lastMyOrder] = append(supplyMap[lastMyOrder], sp)
				for i := range od.Items {
					fmt.Printf("%d: %s, price:%d\n", i, od.Items[i].Name, od.Items[i].Price)
				}
				// send order information to WebSocketClients.
				jsonData, _ := json.Marshal(od)
				mainWs.broadcast <- []byte("menu," + string(jsonData))
				// is OK to end span?
				span.End()
			} else {
				log.Printf("Not last my order %x %#v", lastMyOrder, sp)
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
		Id:   0, // or mode
		Type: order.ItemType_FOOD,
	}
	itb := &order.Item{
		Id:   0, // or mode
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
		"notifyDemand:OrderDemand(Or)",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	/*
		log.Println("Context pln", ctx)
		log.Printf("Context  %#v", ctx)
		log.Printf("SpanFromContext  %#v", trace.SpanFromContext(ctx))
		log.Printf("BaseSpan         %#v", span)
		log.Printf("Context With Span %#v", trace.ContextWithSpan(ctx, span))
	*/
	sc := span.SpanContext()
	js, _ := sc.MarshalJSON()
	//	log.Printf("MarshalJSON of SpanContext %s", string(js))

	opts := &sxutil.DemandOpts{
		Name: "OrderDemand",
		JSON: string(js),
		Cdata: &api.Content{
			Entity: et,
		},
		Context: ctx,
	}
	span.AddEvent("beforeSending")

	lastMyOrder, _ = client.NotifyDemand(opts) // keep lastMyOrder as a order number(id)
	orderMap[lastMyOrder] = make([]*order.Order, 0)
	supplyMap[lastMyOrder] = make([]*api.Supply, 0)
	span.AddEvent("afterSending")
	log.Printf("Send Demand %#v", opts)
	//	log.Printf("SpanEnd! %v", span)
	//	log.Printf("SpanEnd! %#v", span)
	span.End()

}

// should send Complex Demand!
func sendDemand2(client *sxutil.SXServiceClient) {
	// order food and beverage

	itf := &order.Item{
		Id:   1, // and mode
		Type: order.ItemType_FOOD,
	}
	itb := &order.Item{
		Id:   1, // and mode
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
		"notifyDemand:OrderDemand(And)",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	/*
		log.Println("Context pln", ctx)
		log.Printf("Context  %#v", ctx)
		log.Printf("SpanFromContext  %#v", trace.SpanFromContext(ctx))
		log.Printf("BaseSpan         %#v", span)
		log.Printf("Context With Span %#v", trace.ContextWithSpan(ctx, span))
	*/
	sc := span.SpanContext()
	js, _ := sc.MarshalJSON()
	//	log.Printf("MarshalJSON of SpanContext %s", string(js))

	opts := &sxutil.DemandOpts{
		Name: "OrderDemand",
		JSON: string(js),
		Cdata: &api.Content{
			Entity: et,
		},
		Context: ctx,
	}
	span.AddEvent("beforeSending")

	lastMyOrder, _ = client.NotifyDemand(opts) // keep lastMyOrder as a order number(id)
	orderMap[lastMyOrder] = make([]*order.Order, 0)
	supplyMap[lastMyOrder] = make([]*api.Supply, 0)
	span.AddEvent("afterSending")
	log.Printf("Send Demand %#v", opts)
	//	log.Printf("SpanEnd! %v", span)
	//	log.Printf("SpanEnd! %#v", span)
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
			log.Printf("Broadcasting! %d:%s", len(ws.clients), string(message))
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

func orderList() []*order.Order {
	// get current orders
	return orderMap[lastMyOrder]
	/*	keys := make([]uint64, len(orderMap))
		i := 0
		itemLen := 0
		for k := range orderMap {
			keys[i] = k
			itemLen += len(orderMap[k])
			i++
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		items := make([]*order.Item, itemLen)
		i = 0
		for _, k := range keys {
			ods := orderMap[k]
			for _, od := range ods {
				for _, itms := range od.Items {
					items[i] = itms
					i++
				}
			}
		}
		return items
	*/
}

func (c *WClient) messageHandler(msg []byte) {
	// handling message from client
	str := string(msg)
	log.Printf("get message[%s]", str)
	cmds := strings.SplitN(str, ",", 2)
	//	log.Printf("get message[%#v]", cmds)
	switch cmds[0] {
	case "getall":
		lst := orderList()
		for _, itm := range lst {
			jsonData, _ := json.Marshal(itm)
			log.Printf("Response to send %s : %#v", string(jsonData), itm)
			c.send <- []byte("menu," + string(jsonData))
			// may insert sleep.

		}
	case "order":
		itms := make([]*order.Item, 1, 10)
		err := json.Unmarshal([]byte(cmds[1]), &itms)
		if err != nil { // success
			log.Printf("Unmarshal error %v", err)
		} else {
			log.Printf("got order items %d", len(itms))
			for j, ii := range itms {
				log.Printf("   %d: %s", j+1, ii)
			}

			sps := supplyMap[lastMyOrder]
			ods := orderMap[lastMyOrder]
			for i, sp := range sps {
				od := ods[i]         // should be same order in supply..
				od_shops := od.Shops // 特定のオーダーに含まれる店舗
				log.Printf("od_shops %v", od_shops)
				spflag := false
				for _, osp := range od_shops {
					for _, itm := range itms { //
						if osp.ShopId == itm.ShopId {
							spflag = true
							break
						}
						if spflag {
							break
						}
					}
					if spflag {
						break
					}
				}
				log.Printf("Check sp is ok? %v %v %v", spflag, sp, od)
				if spflag { // we should make supply!
					// create new span!
					spanCtx := sxutil.UnmarshalSpanContextJSON(sp.ArgJson)
					log.Printf("argJson")
					ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanCtx)
					_, span := tracer.Start(
						ctx,
						"send:SelectModifiedSupply",
						trace.WithSpanKind(trace.SpanKindClient),
					)

					countOrder := 0
					for _, its := range itms { //受け取った オーダーとの対応
						for _, sit := range od.Items {
							if sit.ShopId == its.ShopId && sit.Name == its.Name {
								sit.Order = its.Order
								log.Printf("Set Order! %d %s %d", sit.ShopId, sit.Name, sit.Order)
								countOrder += 1
							}
						}
					}
					if countOrder > 0 {
						// send supply
						span.AddEvent("Send SelectModifiedSupply")
						bt, err := proto.Marshal(od)
						log.Printf("selectModSupply %#v", geClient)
						// we need to set Order to sp
						if err == nil {
							// mod supply!
							sp.Cdata = &api.Content{
								Entity: bt,
							}
							log.Printf("Supply %#v", sp)
							sid, err2 := geClient.SelectModifiedSupplyWithContext(ctx, sp)
							if err2 != nil {
								log.Printf("Error on select supply %#v", err2)
								mainWs.broadcast <- []byte("failed,")
							} else {
								log.Printf("send SelectSupply OK (Confirmed) %d", sid)
								jsonData, _ := json.Marshal(od)
								mainWs.broadcast <- []byte("confirmed," + string(jsonData))

							}
						} else {
							log.Printf("Error on Marshaling! %#v", od)
						}
					} else { // should not come!
						log.Printf("Should not come here!")
					}

					// send order information to WebSocketClients.
					// is OK to end span?
					span.End()

				}
			}

		}
		// for each proposal -> selectSupply

	}

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
		//		go c.ws.messageHandler(message)
		go c.messageHandler(message)
		//		go c.ws.messageHandler(message)
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

			/* should not do this
			// Add queued chat messages to the current websocket message.
			n := len(c.send) //
			for i := 0; i < n; i++ {
				w.Write(<-c.send)
				}
			*/
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
	log.Printf("WS Connected:%#v", client)
	ws.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

func index(ctx *gin.Context) {
	log.Printf("Get index: %#v", ctx)
	ctx.Header("Access-Control-Allow-Origin", "*")
	ctx.HTML(200, "index.html", gin.H{"data": "Hello"})
}

func main() {
	flag.Parse()
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
	tracer = otel.Tracer("user_provider", trace.WithInstrumentationVersion("up:0.0.1"))
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

	geClient = sxutil.NewSXServiceClient(client, orderChannel, "{Client:UserProvider}")

	wg.Add(1)
	log.Print("Subscribe Supply")
	go subscribeOrderSupply(geClient)

	router := gin.Default()
	// set tepmlate diretory
	templatePath := *tempdir
	if templatePath == "" {
		execBin, _ := os.Executable()
		execPath := filepath.Dir(execBin)
		templatePath = path.Join(execPath, "templates/*.html")
	}
	log.Printf("TemplatePath: %s", templatePath)
	router.Static("/static", "./static")
	router.LoadHTMLGlob(templatePath)

	router.GET("/menu", func(c *gin.Context) {
		log.Printf("Get from %#v", c)
		c.Header("Access-Control-Allow-Origin", "*")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("getMenu"))

		// should clean! Webpage.
		mainWs.broadcast <- []byte("clean,")

		sendDemand(geClient)
		//		index(c)
	})

	router.GET("/menu2", func(c *gin.Context) {
		log.Printf("Get from %#v", c)
		c.Header("Access-Control-Allow-Origin", "*")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("getMenu"))

		// should clean! Webpage.
		mainWs.broadcast <- []byte("clean,")

		sendDemand2(geClient)
		//		index(c)
	})

	mainWs = NewWSServ() // web socket server
	// for websocket
	router.GET("/ws", func(c *gin.Context) {
		servWS(mainWs, c.Writer, c.Request)
	})
	go mainWs.run()

	router.GET("/", index)
	router.Run(":8081")

	wg.Wait()

}
