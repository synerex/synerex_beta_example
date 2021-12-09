package main

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func index(ctx *gin.Context) {
	ctx.Header("Access-Control-Allow-Origin", "*")
	ctx.HTML(200, "index.html", gin.H{"data": "Hello"})
}

func main() {
	router := gin.Default()

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
	router.GET("/", index)

	ts := NewTServ()
	go ts.run()

	// for WebSocket
	router.GET("/ws", func(c *gin.Context) {
		ServeTServWs(ts, c.Writer, c.Request)
	})

	router.Run()
}
