// Package server exposes the game over WebSocket and serves client data/assets over HTTP.
package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/truong-ne/game-nro-clone/server/internal/game"
	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

type Server struct {
	World      *game.World
	Store      *store.Store
	AssetDir   string // legacy SRC/data, served read-only under /assets/
	AdminToken string // bearer token for /admin/*; empty disables the admin API

	limiter  *loginLimiter
	dataGzip []byte
	cache    gzipCache
}

func New(w *game.World, st *store.Store, assetDir string) *Server {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(w.Data.ClientJSON)
	zw.Close()
	return &Server{World: w, Store: st, AssetDir: assetDir, limiter: newLoginLimiter(), dataGzip: buf.Bytes()}
}

// Asset folders the client may fetch. Everything else under data/ stays private.
var assetDirs = map[string]bool{"icon": true, "res": true, "mob": true, "effect": true, "effdata": true,
	"img_by_name": true, "item_bg_temp": true}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.serveWS)
	mux.HandleFunc("GET /api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Write(s.dataGzip)
			return
		}
		w.Write(s.World.Data.ClientJSON)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/map/{id}", s.serveMap)
	mux.HandleFunc("GET /api/mob/{id}", s.serveMob)
	mux.HandleFunc("GET /metrics", s.serveMetrics)
	s.adminRoutes(mux)
	if s.AssetDir != "" {
		files := http.StripPrefix("/assets/", http.FileServer(http.Dir(s.AssetDir)))
		mux.HandleFunc("GET /assets/", func(w http.ResponseWriter, r *http.Request) {
			p := path.Clean(strings.TrimPrefix(r.URL.Path, "/assets/"))
			top, _, _ := strings.Cut(p, "/")
			if !assetDirs[top] || strings.HasSuffix(p, "/") || strings.Contains(p, "..") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=86400")
			files.ServeHTTP(w, r)
		})
	}
	return mux
}

const (
	maxMsgBytes = 16 << 10
	idleTimeout = 60 * time.Second
	sendQueue   = 256
	msgsPerSec  = 40
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{4,20}$`)

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	c.SetReadLimit(maxMsgBytes)
	metricConns.Add(1)
	defer metricConns.Add(-1)
	conn := newConn(c)
	sess := &clientSession{srv: s, conn: conn, ip: clientIP(r)}
	sess.run(r.Context())
}

// wsConn implements game.Conn with a bounded outgoing queue.
type wsConn struct {
	c      *websocket.Conn
	out    chan []byte
	done   chan struct{}
	once   sync.Once
	reason string
}

func newConn(c *websocket.Conn) *wsConn {
	w := &wsConn{c: c, out: make(chan []byte, sendQueue), done: make(chan struct{})}
	go w.writer()
	return w
}

func (w *wsConn) writer() {
	for {
		select {
		case msg := <-w.out:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := w.c.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				w.Close("write failed")
				return
			}
		case <-w.done:
			// Flush what is queued (e.g. a kick notice) before closing.
			for {
				select {
				case msg := <-w.out:
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					w.c.Write(ctx, websocket.MessageText, msg)
					cancel()
				default:
					w.c.Close(websocket.StatusNormalClosure, w.reason)
					return
				}
			}
		}
	}
}

func (w *wsConn) Send(msg []byte) bool {
	select {
	case <-w.done:
		return true
	default:
	}
	select {
	case w.out <- msg:
		metricMsgsOut.Add(1)
		return true
	default:
		metricDropped.Add(1)
		return false
	}
}

func (w *wsConn) Close(reason string) {
	w.once.Do(func() {
		w.reason = reason
		close(w.done)
	})
}

type clientSession struct {
	srv     *Server
	conn    *wsConn
	ip      string
	account *store.Account
	player  *game.Player
}

func (cs *clientSession) send(t string, d any) { cs.conn.Send(game.Encode(t, d)) }

func (cs *clientSession) fail(msg string) { cs.send("error", map[string]string{"text": msg}) }

func (cs *clientSession) run(ctx context.Context) {
	defer func() {
		if cs.player != nil {
			cs.srv.World.Leave(cs.player)
		}
		cs.conn.Close("bye")
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-cs.conn.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	cs.send("hello", map[string]any{"version": 1})
	windowStart, count := time.Now(), 0
	for {
		rctx, rcancel := context.WithTimeout(ctx, idleTimeout)
		_, data, err := cs.conn.c.Read(rctx)
		rcancel()
		if err != nil {
			return
		}
		if now := time.Now(); now.Sub(windowStart) > time.Second {
			windowStart, count = now, 0
		}
		metricMsgsIn.Add(1)
		if count++; count > msgsPerSec {
			cs.conn.Close("flood")
			return
		}
		var env game.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		cs.handle(ctx, env)
	}
}

func (cs *clientSession) handle(ctx context.Context, env game.Envelope) {
	if env.T == "ping" {
		cs.send("pong", nil)
		return
	}
	if p := cs.player; p != nil {
		if z := p.Zone(); z != nil {
			t, d := env.T, env.D
			z.Post(func() { z.Handle(p, t, d) })
		}
		return
	}
	switch env.T {
	case "register", "login":
		if cs.account != nil {
			return
		}
		var m struct{ Username, Password string }
		if json.Unmarshal(env.D, &m) != nil {
			return
		}
		cs.auth(ctx, env.T == "register", m.Username, m.Password)
	case "create_char":
		if cs.account == nil {
			return
		}
		var m struct {
			Name   string
			Gender int
			Head   int
		}
		if json.Unmarshal(env.D, &m) != nil {
			return
		}
		c, err := game.NewCharacter(cs.account.ID, m.Name, m.Gender, m.Head)
		if err != nil {
			cs.fail(err.Error())
			return
		}
		if err := cs.srv.Store.CreateCharacter(ctx, c); err != nil {
			if errors.Is(err, store.ErrTaken) {
				cs.fail("Tên nhân vật đã tồn tại")
			} else {
				slog.Error("create character", "err", err)
				cs.fail("Lỗi máy chủ")
			}
			return
		}
		cs.enter(c)
	}
}

func (cs *clientSession) auth(ctx context.Context, register bool, username, password string) {
	if !cs.srv.limiter.allow(cs.ip) {
		cs.fail("Đăng nhập sai quá nhiều lần, thử lại sau")
		return
	}
	if !usernameRe.MatchString(username) || len(password) < 6 || len(password) > 64 {
		cs.fail("Tài khoản 4-20 ký tự (chữ, số, _), mật khẩu 6-64 ký tự")
		return
	}
	var acc *store.Account
	var err error
	if register {
		acc, err = cs.srv.Store.Register(ctx, username, password)
	} else {
		acc, err = cs.srv.Store.Login(ctx, username, password, cs.ip)
	}
	switch {
	case errors.Is(err, store.ErrTaken):
		cs.fail("Tài khoản đã tồn tại")
		return
	case errors.Is(err, store.ErrBadLogin):
		cs.srv.limiter.fail(cs.ip)
		metricLoginFail.Add(1)
		cs.fail("Sai tài khoản hoặc mật khẩu")
		return
	case errors.Is(err, store.ErrBanned):
		cs.fail("Tài khoản đã bị khoá")
		return
	case err != nil:
		slog.Error("auth", "err", err)
		cs.fail("Lỗi máy chủ")
		return
	}
	cs.srv.limiter.reset(cs.ip)
	cs.account = acc
	cs.srv.World.TakeOver(acc.ID)
	c, err := cs.srv.Store.LoadCharacter(ctx, acc.ID)
	if errors.Is(err, store.ErrNotFound) {
		cs.send("need_char", map[string]any{"heads": game.StarterHeads})
		return
	}
	if err != nil {
		slog.Error("load character", "err", err)
		cs.fail("Lỗi máy chủ")
		return
	}
	cs.enter(c)
}

func (cs *clientSession) enter(c *store.Character) {
	cs.player = game.NewPlayer(c, cs.account.IsAdmin, cs.conn, cs.srv.World.Data)
	cs.send("login_ok", map[string]any{"id": c.ID, "name": c.Name})
	cs.srv.World.Join(cs.player)
}

// loginLimiter blocks an IP for a while after repeated failures.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string]*failRec
}

type failRec struct {
	n     int
	until time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{fails: map[string]*failRec{}} }

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[ip]
	return f == nil || time.Now().After(f.until)
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[ip]
	if f == nil {
		f = &failRec{}
		l.fails[ip] = f
	}
	if f.n++; f.n >= 5 {
		f.n = 0
		f.until = time.Now().Add(time.Minute)
	}
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	delete(l.fails, ip)
	l.mu.Unlock()
}
