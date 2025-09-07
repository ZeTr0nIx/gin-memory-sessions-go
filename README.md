```go
type Response struct {
	Count int `json:"count"`
}

func main() {
	router := gin.Default()
	sm := sessions.NewSessionManager()

	router.Use(sm.Handle())
	router.GET("/count", func(c *gin.Context) {
		sess := sessions.GetSession(c)

		counter := sess.Get("counter")
		if counter == nil {
			sess.Put("counter", 1)
			c.JSON(200, Response{1})
			return
		}
		count := counter.(int) + 1
		sess.Put("counter", count)
		c.JSON(200, Response{count})
	})

	router.Run(":8080")
}
```
