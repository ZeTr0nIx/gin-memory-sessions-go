# Example
```go
type Response struct {
	Count int `json:"count"`
}

func main() {
	sm := session.NewSessionManager()
	r := gin.New()
	ep := r.Group("", sm.Handle())

	ep.Handle("GET", "test", func(c *gin.Context) {
        // retrieve session from gin context
		sess := session.GetSession(c)
        
        // retrieve value from session
		count := 0
		if countVal := sess.Get("count"); countVal != nil {
			count = countVal.(int)
		}
		//or
		count, _ := session.GetGenericValue[int](sess, "count")

		count++

        // update store new value in session
		sess.Put("count", count)

		c.JSON(200, count)
		c.Done()
	})

	if err := r.Run(":4200"); err != nil {
		panic(err)
	}
}
```

