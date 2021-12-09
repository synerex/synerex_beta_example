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
)

var (
	nodesrv         = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	localsx         = flag.String("local", "", "Local Synerex Server")
	mu              sync.Mutex
	sxServerAddress string
)

const orderChannel uint32 = 1 // just for private
var lastMyOrder uint64

func supplyOrderCallback(clt *sxutil.SXServiceClient, sp *api.Supply) {
	od := &order.Order{}
	dt := sp.Cdata.Entity
	err := proto.Unmarshal(dt, od)
	if err == nil {
		log.Printf("got order! %s %#v", sp.SupplyName, *od)
		if sp.SupplyName == "SupplyOrder" { // check the order for me!
			if sp.TargetId == lastMyOrder { // this is supply for my order
				// list up orders for selection.
				fmt.Printf("Get %d Items!\n", len(od.Items))
				for i := range od.Items {
					fmt.Printf("%d: %s, price:%d\n", i, od.Items[i].Name, od.Items[i].Price)
				}
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

	opts := &sxutil.DemandOpts{
		Name: "OrderDemand",
		Cdata: &api.Content{
			Entity: et,
		},
	}

	lastMyOrder, _ = client.NotifyDemand(opts)
	log.Printf("Send Demand %#v", opts)

}

func servWS(ts *TServ, w http.ResponseWriter, r *http.Request) {
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

func main() {
	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)
	log.Printf("User Provider(%s) built %s sha1 %s", sxutil.GitVer, sxutil.BuildTime, sxutil.Sha1Ver)

	channelTypes := []uint32{orderChannel} // use sample channel type

	var rerr error
	sxServerAddress, rerr = sxutil.RegisterNode(*nodesrv, "UserProvider", channelTypes, nil)

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

	// for websocket
	router.GET("/ws", func(c *gin.Context) {
		serveWS(ts, c.Writer, c.Request)
	})

	router.Run(":8081")

	wg.Wait()

}
