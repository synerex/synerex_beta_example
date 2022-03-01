package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	proto "google.golang.org/protobuf/proto"

	api "github.com/synerex/synerex_api"
	order "github.com/synerex/synerex_beta_example/proto_order"
	sxutil "github.com/synerex/synerex_sxutil"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Integration Provider which integrates Food and Beverage

// Product info for service
type Product struct {
	Name   string
	Count  int // stock
	Type   order.ItemType
	Price  int
	LastSp uint64
	Sp1ix  int
	Sp2ix  int
}

var (
	nodesrv               = flag.String("nodesrv", "127.0.0.1:9990", "Node ID Server")
	localsx               = flag.String("local", "", "Local Synerex Server")
	pvname                = flag.String("name", "IntgPrvdr", "Set provider name")
	oid             int32 = 100
	prodlist        []*Product
	propMap         map[uint64]*sxutil.SupplyOpts
	tracer          trace.Tracer
	sxServerAddress string
	supplyChan      chan uint64
	selectChan      chan bool
	lastDemand      *api.Demand
)

const orderChannel uint32 = 1 // just for private

var lastMyOrder uint64 // for keep last "NofityDemand" id.

// map between "NotifyDemand" and its reply("ProposeSupply") orders
var orderMap map[uint64][]*order.Order = make(map[uint64][]*order.Order)
var supplyMap map[uint64][]*api.Supply = make(map[uint64][]*api.Supply)

func sendOrDemand(client *sxutil.SXServiceClient) {
	// order food or beverage
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

	// start Trace with demand context
	//	ctx := context.Background()
	spanCtx := sxutil.UnmarshalSpanContextJSON(lastDemand.ArgJson)
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanCtx)
	var span trace.Span
	ctx, span = tracer.Start(
		ctx,
		"notifyDemand:OrderDemand(Or)",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	sc := span.SpanContext()
	js, _ := sc.MarshalJSON()
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
	log.Printf("Send Or Demand %#v", opts)
	span.End()

	supplyChan = make(chan uint64)
	go waitSupply(client, supplyChan, 2)
}

func min(a, b int32) int {
	if a < b {
		return int(a)
	}
	return int(b)
}

// make  Products
func makeSubProd(lastSp uint64, ord1 []*order.Item, ord2 []*order.Item) []*Product {
	p := []*Product{}
	for i := range ord1 {
		for j := range ord2 {
			p = append(p, &Product{
				Name:   ord1[i].Name + "-" + ord2[j].Name,
				Count:  min(ord1[i].Stock, ord2[j].Stock),
				Type:   order.ItemType_ANY,
				Price:  int(ord1[i].Price + ord2[j].Price),
				LastSp: lastSp,
				Sp1ix:  i,
				Sp2ix:  j,
			})
		}
	}
	return p
}

func waitSupply(client *sxutil.SXServiceClient, ch chan uint64, count int) {
	var sp uint64
	ll := count
	for ll > 0 {
		sp = <-ch
		ll--
	}
	close(ch)
	ch = nil
	orders := orderMap[sp]
	if len(orders) < count {
		log.Printf("something wrong..")
		return
	}

	// now we got several orders

	//make product!
	prodlist = makeSubProd(sp, orders[0].Items, orders[1].Items)

	itms := []*order.Item{}
	// uint64(clt.ClientID)  <- Javascript only arrows 2^53bit.
	shopID := client.GetNodeId()

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

	spanCtx := sxutil.UnmarshalSpanContextJSON(lastDemand.ArgJson)
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
		Target: lastDemand.Id,
		Name:   "SupplyOrder",
		Cdata: &api.Content{
			Entity: en,
		},
		Context: ctx,
	}
	//	log.Printf("Send propose ID:%d  %#v ", client.ClientID, *spo)
	//		log.Printf("Send propose %#v ", *spo)
	sid := client.ProposeSupply(spo)
	span.AddEvent("afterSend")
	// keep sid and spo for future
	propMap[sid] = spo
	span.End()
}

func sendSelectSupply(clt *sxutil.SXServiceClient, prod *Product, selectChan chan bool, dm *api.Demand, orderCount int) {
	spanCtx := sxutil.UnmarshalSpanContextJSON(dm.ArgJson)
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), spanCtx)
	_, span := tracer.Start(
		ctx,
		"send:SelectModifiedSupply",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	spm := supplyMap[prod.LastSp]
	odm := orderMap[prod.LastSp]

	odm[0].Items[prod.Sp1ix].Order = int32(orderCount)
	bt, _ := proto.Marshal(odm[0])
	spm[0].Cdata = &api.Content{
		Entity: bt,
	}
	span.AddEvent("beforeSend")
	sid, err2 := clt.SelectModifiedSupplyWithContext(ctx, spm[0])
	if err2 != nil {
		log.Printf("Error on select supply %#v", err2)
		selectChan <- false
		span.AddEvent("error Context")
		span.End()
		return
	} else {
		log.Printf("send SelectSupply OK (Confirmed) %d", sid)
	}
	span.AddEvent("afterSend")
	span.End()

	odm[1].Items[prod.Sp2ix].Order = int32(orderCount)
	bt2, _ := proto.Marshal(odm[1])
	spm[1].Cdata = &api.Content{
		Entity: bt2,
	}
	span.AddEvent("beforeSend")
	sid2, err3 := clt.SelectModifiedSupplyWithContext(ctx, spm[1])
	if err3 != nil {
		log.Printf("Error on select supply %#v", err2)
		selectChan <- false
		span.AddEvent("error Context")
		span.End()
		return
	} else {
		log.Printf("send SelectSupply OK (Confirmed) %d", sid2)
	}
	span.AddEvent("afterSend")
	span.End()
	selectChan <- true
}

func waitSelectSupply(selectChan chan bool) bool {
	tf := <-selectChan
	close(selectChan)
	return tf
}

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
									// we need to select each item for other services.
									selectChan = make(chan bool)
									go sendSelectSupply(clt, prod, selectChan, dm, int(itm.Order))
									if waitSelectSupply(selectChan) { // confirm both!
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

									}

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
			andFlag := true
			for _, item := range myOrder.Items {
				if item.Id == 0 {
					andFlag = false
				}
			}
			if !andFlag { // and mode. (Need both bev and food)
				return
			}
			// all item should be "And mode"
			lastDemand = dm
			sendOrDemand(clt)

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

// order supply from other Services.
func supplyOrderCallback(clt *sxutil.SXServiceClient, sp *api.Supply) {
	od := &order.Order{}
	dt := sp.Cdata.Entity
	//	log.Printf("Got Supply #%v", sp)
	err := proto.Unmarshal(dt, od)
	if err == nil {
		if sp.SupplyName == "SupplyOrder" { // check the order for me!
			if sp.TargetId == lastMyOrder { // this is supply for my order
				spanCtx := sxutil.UnmarshalSpanContextJSON(sp.ArgJson)
				_, span := tracer.Start(
					trace.ContextWithRemoteSpanContext(context.Background(), spanCtx),
					"receive:SupplyOrder",
					trace.WithSpanKind(trace.SpanKindClient),
				)
				orderMap[lastMyOrder] = append(orderMap[lastMyOrder], od)
				supplyMap[lastMyOrder] = append(supplyMap[lastMyOrder], sp)
				if supplyChan != nil {
					supplyChan <- lastMyOrder
				}
				for i := range od.Items {
					fmt.Printf("%d: %s, price:%d\n", i, od.Items[i].Name, od.Items[i].Price)
				}
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

func main() {
	flag.Parse()
	log.Printf("%s(%s) built %s sha1 %s", *pvname, sxutil.GitVer, sxutil.BuildTime, sxutil.Sha1Ver)
	go sxutil.HandleSigInt()
	sxutil.RegisterDeferFunction(sxutil.UnRegisterNode)
	// check type

	prodlist = []*Product{}

	propMap = make(map[uint64]*sxutil.SupplyOpts)

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

	go subscribeOrderSupply(odClient)

	wg.Wait()

}
