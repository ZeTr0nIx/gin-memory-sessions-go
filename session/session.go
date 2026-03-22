// Package session contains Classes needed to integrate a memory based session storage for https://github.com/gin-gonic/gin.
// It uses a cookie to store the session name which is used to retrieve the session from the store
package session

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Session struct {
	mu             sync.RWMutex
	createdAt      time.Time
	lastActivityAt time.Time
	id             string
	data           *sync.Map
}

type SessionStore interface {
	read(id string) *Session
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

type sessionContextWriter struct {
	sessionManager *SessionManager
	c              *gin.Context
	done           bool
	domain         string
}

type expSession struct {
	Id             string
	Data           map[string]any
	CreatedAt      time.Time
	LastActivityAt time.Time
}
type Option func(*SessionManager)

var logger = func() *log.Logger {
	logger := log.Default()
	logger.SetPrefix("[gin-memory-sessions-go]")
	return logger
}()

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
		data:           &sync.Map{},
		createdAt:      time.Now(),
		lastActivityAt: time.Now(),
	}
}

func GetGenericValue[T any](session *Session, key string) (T, error) {
	session.touch()
	if val, ok := session.data.Load(key); ok {
		return val.(T), nil
	}
	return *new(T), fmt.Errorf("no value found for key: %s", key)
}

func (s *Session) Get(key string) any {
	s.touch()
	if val, ok := s.data.Load(key); ok {
		return val
	}
	return nil
}

func (s *Session) Put(key string, value any) {
	s.touch()
	s.data.Store(key, value)
}

func (s *Session) Delete(key string) {
	s.touch()
	s.data.Delete(key)
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastActivityAt = time.Now()
	s.mu.Unlock()
}

func (s *Session) getLastActivity() time.Time {
	s.mu.RLock()
	t := s.lastActivityAt
	s.mu.RUnlock()
	return t
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
		err := m.store.gc(m.idleExpiration, m.absoluteExpiration)
		if err != nil {
			panic(err)
		}
	}
}

func (m *SessionManager) validate(session *Session) bool {
	if time.Since(session.createdAt) > m.absoluteExpiration ||
		time.Since(session.getLastActivity()) > m.idleExpiration {

		// Delete the session from the store
		err := m.store.destroy(session.id)
		if err != nil {
			return false
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
		session = m.store.read(cookie)
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
	session.touch()

	err := m.store.write(session)
	if err != nil {
		return err
	}

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

		// Write the session cookie to the response if not already written
		writeCookieIfNecessary(sw)

		// Call the next handler and pass the new response writer and new request
		c.Next()
		err := m.save(session)
		if err != nil {
			logger.Println(err)
			errr := c.Error(err)
			if errr != nil {
				logger.Print(errr.Error())
			}
		}
	}
}

type fileStore struct {
	mu       sync.RWMutex
	fileName string
}

// Creates a file to store sessions for testing.
//
// Should not be used in production!!!
func NewFileStore(file string) *fileStore {
	return &fileStore{
		mu:       sync.RWMutex{},
		fileName: file,
	}
}

func (f *fileStore) read(id string) *Session {
	f.mu.RLock()
	defer f.mu.RUnlock()

	data, err := os.ReadFile(f.fileName)
	if err != nil {
		return nil
	}
	m := make(map[string]any)
	if len(data) != 0 {
		err = json.Unmarshal(data, &m)
		if err != nil {
			return nil
		}
	}

	data, err = json.Marshal(m[id])
	if err != nil {
		return nil
	}

	var expS expSession
	err = json.Unmarshal(data, &expS)
	if err != nil {
		return nil
	}
	m2 := &sync.Map{}
	for k, v := range expS.Data {
		m2.Store(k, v)
	}
	return &Session{
		id:             expS.Id,
		createdAt:      expS.CreatedAt,
		lastActivityAt: expS.LastActivityAt,
		data:           m2,
	}

}

func (f *fileStore) write(session *Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	file, err := openFile(f.fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := os.ReadFile(f.fileName)
	if err != nil {
		return err
	}
	m := make(map[string]any)
	if len(data) != 0 {
		err = json.Unmarshal(data, &m)
		if err != nil {
			return err
		}
	}

	m2 := make(map[string]any)
	session.data.Range(func(key any, value any) bool {
		m2[key.(string)] = value
		return true
	})

	m[session.id] = expSession{
		Id:             session.id,
		Data:           m2,
		CreatedAt:      session.createdAt,
		LastActivityAt: session.lastActivityAt,
	}

	data, err = json.Marshal(m)
	if err != nil {
		return err
	}

	_, err = file.Write(data)
	if err != nil {
		return err
	}
	return nil
}
func (f *fileStore) destroy(id string) error {
	return nil
}
func (f *fileStore) gc(idleExpiration, absoluteExpiration time.Duration) error {
	return nil
}

type inMemorySessionStore struct {
	mu       sync.RWMutex
	sessions *sync.Map
}

func NewInMemorySessionStore() *inMemorySessionStore {
	return &inMemorySessionStore{
		mu:       sync.RWMutex{},
		sessions: &sync.Map{},
	}
}

func GetSession(c *gin.Context) *Session {
	session, ok := c.Value("session").(*Session)
	if !ok {
		panic("session not found in request context")
	}

	return session
}

func (s *inMemorySessionStore) read(id string) *Session {
	if session, ok := s.sessions.Load(id); ok {
		return session.(*Session)
	}
	return nil
}

func (s *inMemorySessionStore) write(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions.Store(session.id, session)

	return nil
}

func (s *inMemorySessionStore) destroy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions.Delete(id)

	return nil
}

func (s *inMemorySessionStore) gc(idleExpiration, absoluteExpiration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions.Range(func(key, value any) bool {
		session := value.(*Session)
		if time.Since(session.getLastActivity()) > idleExpiration ||
			time.Since(session.createdAt) > absoluteExpiration {
			s.sessions.Delete(key)
			return false
		}
		return true
	})
	return nil
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

func openFile(name string) (*os.File, error) {
	file, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_RDONLY, 0660)
	if err != nil {
		if os.IsNotExist(err) {
			file, err = os.Create(name)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return file, nil
}
