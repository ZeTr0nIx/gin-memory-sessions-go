package main

import (
	"github.com/gin-gonic/gin"
	"github.com/zetr0nix/gin-memory-sessions-go/session"
)

func main() {
	sm := session.NewSessionManager()
	r := gin.New()
	ep := r.Group("", sm.Handle())

	ep.Handle("GET", "test", func(c *gin.Context) {
		sess := session.GetSession(c)
		count := 0
		if countVal := sess.Get("count"); countVal != nil {
			count = countVal.(int)
		}

		count++
		sess.Put("count", count)

		c.JSON(200, count)
		c.Done()
	})

	if err := r.Run(":4200"); err != nil {
		panic(err)
	}
}
