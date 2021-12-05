package main

import (
	"github.com/gin-gonic/gin"
)

func index(ctx *gin.Context) {
	ctx.HTML(200, "index.html", gin.H{"data": "Hello"})
}

func main() {
	router := gin.Default()

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
