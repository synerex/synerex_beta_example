package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"

	"go.opentelemetry.io/otel"

	//	stdout "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	// for gin middleware
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

//func index(ctx *gin.Context) {
//	ctx.Header("Access-Control-Allow-Origin", "*")
//	ctx.HTML(200, "index.html", gin.H{"data": "Hello"})
//}

// Init configures an OpenTelemetry exporter and trace provider
func NewOltpTracer() *sdktrace.TracerProvider {
	//	exporter, err := stdout.New(stdout.WithPrettyPrint())
	// Jaeger Exporter is also defined through Env var
	//     OTEL_EXPORTER_JAEGER_AGENT_HOST=127.0.0.1 ,
	//     OTEL_EXPORTER_JAEGER_ENDPOINT=http://127.0.0.1:14268/api/traces
	exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint("http://" + *jaegerHost + ":14268/api/traces")))
	if err != nil {
		log.Fatal("mgr:Can't open jaeger exporter ", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp
}

func main() {
	flag.Parse()

	tp := NewOltpTracer()
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

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
	router.Use(otelgin.Middleware("ex-manager"))
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

	execPath, _ := os.Executable()
	templatePath := path.Join(path.Dir(execPath), "templates/*.html")
	router.LoadHTMLGlob(templatePath)
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
