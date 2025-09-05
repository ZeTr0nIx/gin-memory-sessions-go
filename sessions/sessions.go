// Package sessions contains Classes needed to integrate a memory based session storage for https://github.com/gin-gonic/gin.
// It uses a cookie to store the session name which is used to retrieve the session from the store
package sessions

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Session struct {
	createdAt      time.Time
	lastActivityAt time.Time
	id             string
	data           map[string]any
}
type SessionStore interface {
	read(id string) (*Session, error)
	write(session *Session) error
	destroy(id string) error
	gc(idleExpiration, absoluteExpiration time.Duration) error
}

type SessionManager struct {
	store              SessionStore
	idleExpiration     time.Duration
	absoluteExpiration time.Duration
	cookieName         string
	validationTicker   *time.Ticker
	domain             string
}

type Option func(*SessionManager)

func WithStore(store SessionStore) Option {
	return func(s *SessionManager) {
		s.store = store
	}
}

func WithIdleExpiration(expiration time.Duration) Option {
	return func(s *SessionManager) {
		s.idleExpiration = expiration
	}
}

func WithAbsoluteExpiration(expiration time.Duration) Option {
	return func(s *SessionManager) {
		s.absoluteExpiration = expiration
	}
}

func WithCookieName(cookieName string) Option {
	return func(s *SessionManager) {
		if cookieName == "" {
			panic(errors.New("cookie name cannot be empty"))
		}
		s.cookieName = cookieName
	}
}

func WithCookieDomain(domain string) Option {
	return func(s *SessionManager) {
		s.domain = domain
	}
}

func WithValidationTicker(ticker *time.Ticker) Option {
	return func(s *SessionManager) {
		s.validationTicker = ticker
	}
}

func generateSessionID() string {
	id := make([]byte, 32)

	_, err := io.ReadFull(rand.Reader, id)
	if err != nil {
		panic("failed to generate session id")
	}

	return base64.RawURLEncoding.EncodeToString(id)
}

func newSession() *Session {
	return &Session{
		id:             generateSessionID(),
		data:           make(map[string]any),
		createdAt:      time.Now(),
		lastActivityAt: time.Now(),
	}
}

func (s *Session) Get(key string) any {
	s.lastActivityAt = time.Now()
	return s.data[key]
}

func (s *Session) Put(key string, value any) {
	s.lastActivityAt = time.Now()
	s.data[key] = value
}

func (s *Session) Delete(key string) {
	s.lastActivityAt = time.Now()
	delete(s.data, key)
}

func NewSessionManager(opts ...Option) *SessionManager {
	m := &SessionManager{
		store:              NewInMemorySessionStore(),
		idleExpiration:     10 * time.Minute,
		absoluteExpiration: time.Hour,
		cookieName:         "session",
		domain:             "",
		validationTicker:   time.NewTicker(time.Minute * 5),
	}

	for _, opt := range opts {
		opt(m)
	}

	go m.gc(m.validationTicker)

	return m
}

func (m *SessionManager) gc(t *time.Ticker) {
	for range t.C {
		m.store.gc(m.idleExpiration, m.absoluteExpiration)
	}
}

func (m *SessionManager) validate(session *Session) bool {
	if time.Since(session.createdAt) > m.absoluteExpiration ||
		time.Since(session.lastActivityAt) > m.idleExpiration {

		// Delete the session from the store
		err := m.store.destroy(session.id)
		if err != nil {
			panic(err)
		}

		return false
	}

	return true
}

func (m *SessionManager) start(c *gin.Context) (*Session, *gin.Context) {
	var session *Session

	// Read From Cookie
	cookie, err := c.Cookie(m.cookieName)
	if err == nil {
		session, err = m.store.read(cookie)
		if err != nil {
			log.Printf("Failed to read session from store: %v", err)
		}
	}

	// Generate a new session
	if session == nil || !m.validate(session) {
		session = newSession()
	}
	// Attach session to context
	c.Set("session", session)

	return session, c
}

func (m *SessionManager) save(session *Session) error {
	session.lastActivityAt = time.Now()

	err := m.store.write(session)
	if err != nil {
		return err
	}

	return nil
}

func (m *SessionManager) migrate(session *Session) error {
	err := m.store.destroy(session.id)
	if err != nil {
		return err
	}

	session.id = generateSessionID()

	return nil
}

func (m *SessionManager) Handle() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start the session
		session, c := m.start(c)

		// Create a new response writer
		sw := &sessionContextWriter{
			sessionManager: m,
			c:              c,
			domain:         m.domain,
		}
		// Add essential headers
		c.Header("Vary", "Cookie")
		c.Header("Cache-Control", `no-cache="Set-Cookie"`)

		// Call the next handler and pass the new response writer and new request

		// Save the session
		m.save(session)

		// Write the session cookie to the response if not already written
		writeCookieIfNecessary(sw)
		c.Next()
	}
}

type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		mu:       sync.RWMutex{},
		sessions: make(map[string]*Session),
	}
}

func GetSession(c *gin.Context) *Session {
	session, ok := c.Value("session").(*Session)
	if !ok {
		panic("session not found in request context")
	}

	return session
}

func (s *InMemorySessionStore) read(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session := s.sessions[id]
	return session, nil
}

func (s *InMemorySessionStore) write(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.id] = session

	return nil
}

func (s *InMemorySessionStore) destroy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, id)

	return nil
}

func (s *InMemorySessionStore) gc(idleExpiration, absoluteExpiration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, session := range s.sessions {
		if time.Since(session.lastActivityAt) > idleExpiration ||
			time.Since(session.createdAt) > absoluteExpiration {
			delete(s.sessions, id)
		}
	}

	return nil
}

type sessionContextWriter struct {
	sessionManager *SessionManager
	c              *gin.Context
	done           bool
	domain         string
}

func (w *sessionContextWriter) Write(b []byte) (int, error) {
	writeCookieIfNecessary(w)

	return w.c.Writer.Write(b)
}

func (w *sessionContextWriter) WriteHeader(code int) {
	writeCookieIfNecessary(w)

	w.c.Writer.WriteHeader(code)
}

func (w *sessionContextWriter) Unwrap() http.ResponseWriter {
	return w.c.Writer
}

func writeCookieIfNecessary(w *sessionContextWriter) {
	if w.done {
		return
	}

	session, ok := w.c.Value("session").(*Session)
	if !ok {
		panic("session not found in request context")
	}

	name := w.sessionManager.cookieName
	value := session.id
	domain := w.domain
	httpOnly := true
	path := "/"
	secure := true
	maxAge := int(w.sessionManager.idleExpiration / time.Second)

	w.c.SetCookie(name, value, maxAge, path, domain, secure, httpOnly)
	w.done = true
}
