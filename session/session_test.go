// Package sessions contains Classes needed to integrate a memory based session storage for https://github.com/gin-gonic/gin.
// It uses a cookie to store the session name which is used to retrieve the session from the store
package session

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGoodPath(t *testing.T) {
	rw := httptest.NewRecorder()
	_, router := gin.CreateTestContext(rw)
	store := NewInMemorySessionStore()
	tickerChan := make(chan time.Time)
	ticker := &time.Ticker{
		C: tickerChan,
	}
	sm := NewSessionManager(
		WithStore(store),
		WithValidationTicker(ticker),
	)
	router.Use(sm.Handle())
	sessionID := ""
	values := []string{"A", "B", "C"}
	router.GET("/values", func(ctx *gin.Context) {
		sess := GetSession(ctx)
		sessionID = sess.id
		sess.Put("values", values)
		ctx.Writer.Write([]byte("done"))
	})

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router.Handler(),
	}
	go func() {
		defer log.Println("done")
		// service connections
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
	time.Sleep(2 * time.Second)
	_, err := http.Get("http://localhost:8080/values")
	if err != nil {
		log.Fatalf("%s", err.Error())
	}
	time.Sleep(time.Second)
	log.Println("Shutdown Server ...")
	tickerChan <- time.Now()
	ticker.Stop()
	close(tickerChan)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("Server Shutdown:", err)
	}
	cancel()
	session, err := store.read(sessionID)
	if err != nil {
		log.Fatalf("%s", err.Error())
	}
	assert.Equal(t, values, session.Get("values").([]string))
}

func TestGC(t *testing.T) {
	rw := httptest.NewRecorder()
	_, router := gin.CreateTestContext(rw)
	store := NewInMemorySessionStore()
	tickerChan := make(chan time.Time)
	ticker := &time.Ticker{
		C: tickerChan,
	}
	sm := NewSessionManager(
		WithStore(store),
		WithValidationTicker(ticker),
		WithAbsoluteExpiration(time.Millisecond),
	)
	router.Use(sm.Handle())

	sessionID := ""
	values := []string{"A", "B", "C"}

	router.GET("/values", func(ctx *gin.Context) {
		sess := GetSession(ctx)
		sessionID = sess.id
		sess.Put("values", values)
		ctx.Writer.Write([]byte("done"))
	})

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router.Handler(),
	}
	go func() {
		defer log.Println("done")
		// service connections
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
	time.Sleep(2 * time.Second)
	_, err := http.Get("http://localhost:8080/values")
	if err != nil {
		log.Fatalf("%s", err.Error())
	}
	time.Sleep(time.Second)
	tickerChan <- time.Now()
	log.Println("Shutdown Server ...")
	ticker.Stop()
	close(tickerChan)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := srv.Shutdown(ctx); err != nil {
		log.Println("Server Shutdown:", err)
	}
	cancel()
	session, err := store.read(sessionID)
	if err != nil {
		log.Fatalf("%s", err.Error())
	}
	var nilSess *Session
	assert.Equal(t, nilSess, session)
}

func TestNewSessionManager(t *testing.T) {
	type args struct {
		opts []Option
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "test default new session manager",
			args: args{make([]Option, 0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewSessionManager()
			assert.NotNil(t, got)
		})
	}
}
