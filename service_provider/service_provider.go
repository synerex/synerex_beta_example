package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	proto "google.golang.org/protobuf/proto"

	api "github.com/synerex/synerex_api"
	order "github.com/synerex/synerex_beta_example/proto_order"
	sxutil "github.com/synerex/synerex_sxutil"
)

var (
	nodesrv               = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	localsx               = flag.String("local", "", "Local Synerex Server")
	products              = flag.String("products", "noriben:2,karaage:1", "Products list")
	ptype                 = flag.String("ptype", "food", "Product type")
	pvname                = flag.String("name", "SrvcPrvdr", "Set provider name")
	oid             int32 = 100
	prodlist        []*Product
	propMap         map[uint64]*sxutil.SupplyOpts
	sxServerAddress string
)

const orderChannel uint32 = 1 // just for private

func demandOrderCallback(clt *sxutil.SXServiceClient, dm *api.Demand) {
	myOrder := &order.Order{}
	dt := dm.Cdata.Entity
	log.Printf("Demand! %v", dm)
	err := proto.Unmarshal(dt, myOrder)
	if err == nil {
		log.Printf("got order! %v \n", myOrder)

		// response with Supply
		itms := []*order.Item{}

		for i := range prodlist {
			itms = append(itms, &order.Item{
				Id:     int32(i),
				ShopId: uint64(clt.ClientID),
				Type:   prodlist[i].Type,
				Name:   prodlist[i].Name,
				Stock:  int32(prodlist[i].Count),
				Price:  int32(350 + rand.Intn(10)*20),
			})
		}
		shops := []*order.Shop{
			&order.Shop{
				ShopId: uint64(clt.ClientID),
				Name:   *pvname,
			},
		}

		ord := &order.Order{
			OrderId: oid,
			Shops:   shops,
			Items:   itms,
		}
		oid += 1

		en, _ := proto.Marshal(ord)

		spo := &sxutil.SupplyOpts{
			JSON:   dm.ArgJson,
			Target: dm.Id,
			Name:   "SupplyOrder",
			Cdata: &api.Content{
				Entity: en,
			},
		}
		log.Printf("Send propose %#v ", *spo)
		sid := clt.ProposeSupply(spo)
		// keep sid and spo for future
		propMap[sid] = spo

	}
}

func subscribeOrderDemand(client *sxutil.SXServiceClient) {
	for {
		ctx := context.Background() //
		err := client.SubscribeDemand(ctx, demandOrderCallback)
		log.Printf("Error:Demand %s\n", err.Error())

		time.Sleep(5 * time.Second) // wait 5 seconds to reconnect
		newClt := sxutil.GrpcConnectServer(sxServerAddress)
		if newClt != nil {
			log.Printf("Reconnect server [%s]\n", sxServerAddress)
			client.SXClient = newClt
		}
	}
}

type Product struct {
	Name  string
	Count int
	Type  order.ItemType
}

func main() {
	flag.Parse()
	log.Printf("%s(%s) built %s sha1 %s", *pvname, sxutil.GitVer, sxutil.BuildTime, sxutil.Sha1Ver)
	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)
	// check type
	prodType := order.ItemType_BEVERAGE
	if *ptype == "food" {
		prodType = order.ItemType_FOOD
	}
	prodlist = []*Product{}

	propMap = make(map[uint64]*sxutil.SupplyOpts)

	// set products
	prods := strings.Split(*products, ",")
	fmt.Printf("Product list: %v\n", prods)
	for i := range prods {
		vc := strings.Split(prods[i], ":")
		ct, _ := strconv.Atoi(vc[1])

		prod := &Product{
			Name:  vc[0],
			Count: ct,
			Type:  prodType,
		}
		prodlist = append(prodlist, prod)
		fmt.Printf("%d: %s, %d\n", i, vc[0], ct)
	}

	channelTypes := []uint32{orderChannel} // use sample channel type

	var rerr error
	sxServerAddress, rerr = sxutil.RegisterNode(*nodesrv, *pvname, channelTypes, nil)

	if rerr != nil {
		log.Fatal("Can't register node:", rerr)
	}
	if *localsx != "" { // quick hack for AWS local network
		sxServerAddress = *localsx
	}
	log.Printf("Connecting SynerexServer at [%s]", sxServerAddress)

	tracer := sxutil.NewOltpTracer()
	defer func() {
		log.Printf("Closing oltp traceer")
		if err := tracer.Shutdown(context.Background()); err != nil {
			log.Printf("Can't shoutdown tracer %v", err)
		}
	}()

	wg := sync.WaitGroup{} // for syncing other goroutines

	client := sxutil.GrpcConnectServer(sxServerAddress)

	if client == nil {
		log.Fatal("Can't connect Synerex Server")
	} else {
		log.Print("Connecting SynerexServer")
	}

	odClient := sxutil.NewSXServiceClient(client, orderChannel, "{Client:"+*pvname+"}")

	wg.Add(1)
	log.Print("Subscribe Demand")
	go subscribeOrderDemand(odClient)

	wg.Wait()

}
