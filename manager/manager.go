package main

import (
	"flag"
	"log"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/nkawa/go-gin/ginhttp"
	opentracing "github.com/opentracing/opentracing-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
)

//func index(ctx *gin.Context) {
//	ctx.Header("Access-Control-Allow-Origin", "*")
//	ctx.HTML(200, "index.html", gin.H{"data": "Hello"})
//}

func main() {
	flag.Parse()

	cfg, err := jaegercfg.FromEnv() // get envionment variable
	if err != nil {
		log.Printf("Can't obtain jaeger env vars: %s", err.Error())
	} else {
		log.Printf("Configuration: %#v", cfg)
	}

	tracer, closer, err := cfg.NewTracer()
	//		jaeger.NewConstSampler(true),
	//		jaeger.NewInMemoryReporter(),
	//	)
	if err != nil {
		log.Printf("Can't initialize jaeger trace %s:", err.Error())
	}
	defer closer.Close()

	fn := func(ctx *gin.Context) {
		span := opentracing.SpanFromContext(ctx.Request.Context())
		log.Printf("Now New Span %#v", span)
		if span == nil {
			log.Printf("Span is nil")
		}
		ctx.Header("Access-Control-Allow-Origin", "*")
		ctx.HTML(200, "index.html", gin.H{"data": "Hello"})
	}

	router := gin.New()
	router.Use(ginhttp.Middleware(tracer))
	//  router.Use(gin.LOgger())
	//  router.Use(gin.Recovery())

	group := router.Group("")
	group.GET("", fn)
	//	w := httptest.NewRecorder()
	//	req, err := http.NewRequest("GET", "/", nil)

	//r.ServeHTTP(w, req)

	//	router := gin.Default()

	//	router.Use(cors.Default()) // allow all origin
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://127.0.0.1:8080"},
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Origin"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return origin == "http://127.0.0.1:8080"
		},
		MaxAge: 12 * time.Hour,
	}))
	router.Static("/static", "./static")

	router.LoadHTMLGlob("templates/*.html")
	//	router.GET("/", index)

	// websocket terminal server
	ts := NewTServ()
	go ts.run()

	// for WebSocket
	router.GET("/ws", func(c *gin.Context) {
		ServeTServWs(ts, c.Writer, c.Request)
	})

	router.Run()
}
