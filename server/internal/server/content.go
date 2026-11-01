package server

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/truong-ne/game-nro-clone/server/internal/anim"
)

// gzipCache holds pre-compressed immutable responses keyed by path.
type gzipCache struct{ m sync.Map }

func (c *gzipCache) get(key string, build func() ([]byte, error)) ([]byte, []byte, error) {
	if v, ok := c.m.Load(key); ok {
		e := v.([2][]byte)
		return e[0], e[1], nil
	}
	raw, err := build()
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()
	c.m.Store(key, [2][]byte{raw, buf.Bytes()})
	return raw, buf.Bytes(), nil
}

func writeJSON(w http.ResponseWriter, r *http.Request, raw, gz []byte, immutable bool) {
	w.Header().Set("Content-Type", "application/json")
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(gz)
		return
	}
	w.Write(raw)
}

// serveMap returns one map template. The URL carries ?v=<version>, so it is
// cached forever by clients and proxies.
func (s *Server) serveMap(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || s.World.MapJSON(id) == nil {
		http.NotFound(w, r)
		return
	}
	raw, gz, _ := s.cache.get("map/"+strconv.Itoa(id), func() ([]byte, error) { return s.World.MapJSON(id), nil })
	writeJSON(w, r, raw, gz, r.URL.Query().Get("v") == s.World.MapVersion(id))
}

type mobResponse struct {
	*anim.Mob
	Sheet string `json:"sheet"` // base64 PNG
}

// serveMob decodes a legacy mob file (binary or text variant) into JSON with
// the sprite sheet inlined, so the client never parses the legacy format.
func (s *Server) serveMob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	zoom, _ := strconv.Atoi(r.URL.Query().Get("z"))
	if err != nil || id < 0 || zoom < 1 || zoom > 4 || s.AssetDir == "" {
		http.NotFound(w, r)
		return
	}
	raw, gz, err := s.cache.get(fmt.Sprintf("mob/%d/%d", zoom, id), func() ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(s.AssetDir, "mob", fmt.Sprintf("x%d", zoom), strconv.Itoa(id)))
		if err != nil {
			return nil, err
		}
		m, err := anim.ParseMob(b, id)
		if err != nil {
			return nil, err
		}
		scale := zoom
		if m.SheetScale > 0 {
			scale = m.SheetScale
		}
		m.SheetScale = scale
		m.FillSizes(scale)
		return json.Marshal(mobResponse{m, base64.StdEncoding.EncodeToString(m.PNG)})
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, r, raw, gz, true)
}
