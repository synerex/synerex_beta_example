package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	proto "google.golang.org/protobuf/proto"

	api "github.com/synerex/synerex_api"
	order "github.com/synerex/synerex_beta_example/proto_order"
	sxutil "github.com/synerex/synerex_sxutil"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Product info for service
type Product struct {
	Name  string
	Count int // stock
	Type  order.ItemType
	Price int
}

var (
	nodesrv               = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	localsx               = flag.String("local", "", "Local Synerex Server")
	products              = flag.String("products", "noriben:360:2,karaage:450:1", "Products list:price:stock")
	ptype                 = flag.String("ptype", "food", "Product type")
	pvname                = flag.String("name", "SrvcPrvdr", "Set provider name")
	oid             int32 = 100
	prodlist        []*Product
	prodType        order.ItemType
	propMap         map[uint64]*sxutil.SupplyOpts
	tracer          trace.Tracer
	sxServerAddress string
)

const orderChannel uint32 = 1 // just for private

func demandOrderCallback(clt *sxutil.SXServiceClient, dm *api.Demand) {
	myOrder := &order.Order{}
	dt := dm.Cdata.Entity
	log.Printf("Demand! %v", dm)
	err := proto.Unmarshal(dt, myOrder)
	if err == nil {
		//		log.Printf("got order! %v \n", myOrder)

		// we need to check "SelectSupply"
		if dm.DemandName == "SupplyOrder" { // this is selectModifiedSupply
			if dm.SenderId == uint64(clt.ClientID) {
				log.Printf("got Modified Supply for Me %s\n", myOrder)
				// check order and send "Confirm"

				for _, itm := range myOrder.Items {
					if itm.Order > 0 {
						for _, prod := range prodlist {
							if prod.Name == itm.Name {
								// we should lock! here to make it atomic.
								if prod.Count >= int(itm.Order) {
									prod.Count -= int(itm.Order)
									// OK!
									spanCtx := sxutil.UnmarshalSpanContextJSON(dm.ArgJson)
									ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanCtx)
									_, span := tracer.Start(
										ctx,
										"send:Confirm",
										trace.WithSpanKind(trace.SpanKindClient),
									)
									span.AddEvent("beforeSend")
									clt.ConfirmWithContext(ctx, sxutil.IDType(dm.Id), sxutil.IDType(dm.TargetId))
									span.AddEvent("afterSend")
									span.End()

								} else { // to many order!
									log.Printf("Order exceeds stock!")
									//can't confirm!
									spanCtx := sxutil.UnmarshalSpanContextJSON(dm.ArgJson)
									ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanCtx)
									_, span := tracer.Start(
										ctx,
										"send:CantConfirm",
										trace.WithSpanKind(trace.SpanKindClient),
									)
									span.End()
								}
							}
						}

					}
				}

			}
		} else if dm.DemandName == "OrderDemand" { // demand from user_proivder
			// we need to check the order is "Or" mode.
			andFlag := true
			for _, item := range myOrder.Items {
				if item.Type == prodType && item.Id == 0 {
					andFlag = false
				}
			}
			if andFlag { // and mode. (Need both bev and food)
				return
			}

			// response with Supply
			itms := []*order.Item{}
			// uint64(clt.ClientID)  <- Javascript only arrows 2^53bit.
			shopID := clt.GetNodeId()

			for i := range prodlist {
				if prodlist[i].Count > 0 { // if there is a stock
					itms = append(itms, &order.Item{
						Id:     int32(i),
						ShopId: uint64(shopID), // for javascript
						Type:   prodlist[i].Type,
						Name:   prodlist[i].Name,
						Stock:  int32(prodlist[i].Count),
						Price:  int32(prodlist[i].Price),
					})
				}
			}
			if len(itms) == 0 {
				return // can't do anything...
			}
			shops := []*order.Shop{
				&order.Shop{
					ShopId: uint64(shopID),
					Name:   *pvname,
				},
			}

			ord := &order.Order{
				OrderId: oid,
				Shops:   shops,
				Items:   itms,
			}
			oid += 1

			spanCtx := sxutil.UnmarshalSpanContextJSON(dm.ArgJson)
			ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanCtx)
			_, span := tracer.Start(
				ctx,
				"send:SupplyOrder",
				trace.WithSpanKind(trace.SpanKindClient),
			)
			span.AddEvent("beforeSend")
			en, _ := proto.Marshal(ord)

			js, _ := span.SpanContext().MarshalJSON()

			spo := &sxutil.SupplyOpts{
				JSON:   string(js),
				Target: dm.Id,
				Name:   "SupplyOrder",
				Cdata: &api.Content{
					Entity: en,
				},
				Context: ctx,
			}
			log.Printf("Send propose ID:%d  %#v ", clt.ClientID, *spo)
			//		log.Printf("Send propose %#v ", *spo)
			sid := clt.ProposeSupply(spo)
			span.AddEvent("afterSend")
			// keep sid and spo for future
			propMap[sid] = spo
			span.End()
		} else {
			log.Printf("Can't understand demand %#v", dm)
		}
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

func main() {
	flag.Parse()
	log.Printf("%s(%s) built %s sha1 %s", *pvname, sxutil.GitVer, sxutil.BuildTime, sxutil.Sha1Ver)
	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)
	// check type
	prodType = order.ItemType_BEVERAGE
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
		pr, _ := strconv.Atoi(vc[1])
		ct, _ := strconv.Atoi(vc[2])

		prod := &Product{
			Name:  vc[0],
			Count: ct,
			Type:  prodType,
			Price: pr,
		}
		prodlist = append(prodlist, prod)
		fmt.Printf("%d: %s, %d, %d\n", i, vc[0], pr, ct)
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

	tp := sxutil.NewOltpTracer()
	defer func() {
		log.Printf("Closing oltp traceer")
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Can't shoutdown tracer %v", err)
		}
	}()
	tracer = otel.Tracer(*pvname, trace.WithInstrumentationVersion("sp:0.0.1"))

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
