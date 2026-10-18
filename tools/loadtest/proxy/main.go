package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const upstreamURL = "http://lansurfer:8080"

type control struct {
	Mode       string `json:"mode"`
	DurationMS int    `json:"duration_ms,omitempty"`
	DelayMS    int    `json:"delay_ms,omitempty"`
	DropCount  int    `json:"drop_count,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	EventType  string `json:"event_type,omitempty"`
}
type state struct {
	mu         sync.Mutex
	mode       string
	until      time.Time
	cfg        control
	streams    map[uint64]context.CancelFunc
	nextStream uint64
	seen       atomic.Int64
	delivered  atomic.Int64
	dropped    atomic.Int64
}

func (s *state) registerStream(cancel context.CancelFunc) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextStream++
	if s.streams == nil {
		s.streams = make(map[uint64]context.CancelFunc)
	}
	s.streams[s.nextStream] = cancel
	return s.nextStream
}
func (s *state) unregisterStream(id uint64) {
	s.mu.Lock()
	delete(s.streams, id)
	s.mu.Unlock()
}
func (s *state) cutStreams() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.streams))
	for _, cancel := range s.streams {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
func (s *state) active() control {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.until.IsZero() && time.Now().After(s.until) {
		s.mode = "normal"
		s.until = time.Time{}
		s.cfg = control{Mode: "normal"}
		_, _ = exec.Command("tc", "qdisc", "del", "dev", "eth0", "root").CombinedOutput()
	}
	return s.cfg
}

const proxyMaxIdleConnections = 512

func newReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = proxyMaxIdleConnections
	transport.MaxIdleConnsPerHost = proxyMaxIdleConnections
	proxy.Transport = transport
	return proxy
}

func main() {
	u, _ := url.Parse(upstreamURL)
	s := &state{mode: "normal", cfg: control{Mode: "normal"}}
	proxy := newReverseProxy(u)
	defer proxy.Transport.(*http.Transport).CloseIdleConnections()
	proxy.ModifyResponse = func(r *http.Response) error {
		cfg := s.active()
		if cfg.Mode == "fail-refresh" && strings.Contains(r.Request.URL.Path, "/refresh") {
			r.StatusCode = 503
			r.Status = "503 Service Unavailable"
			r.Body = io.NopCloser(strings.NewReader(""))
			r.ContentLength = 0
			return nil
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") || cfg.Mode != "drop-sse" {
			return nil
		}
		original := r.Body
		r.Body = struct{ io.ReadCloser }{ReadCloser: newFrameFilter(original, cfg, s)}
		return nil
	}
	httpSrv := &http.Server{Addr: ":8080", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.active()
		if cfg.Mode == "offline" {
			http.Error(w, "offline", 503)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/events/") {
			ctx, cancel := context.WithCancel(r.Context())
			id := s.registerStream(cancel)
			defer func() { s.unregisterStream(id); cancel() }()
			proxy.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if cfg.Mode == "hold-refresh" && strings.Contains(r.URL.Path, "/refresh") {
			rr := httptestRecorder{header: make(http.Header)}
			proxy.ServeHTTP(&rr, r)
			time.Sleep(time.Duration(cfg.DelayMS) * time.Millisecond)
			for k, v := range rr.header {
				for _, x := range v {
					w.Header().Add(k, x)
				}
			}
			w.WriteHeader(rr.status)
			_, _ = w.Write([]byte(rr.body.String()))
			return
		}
		proxy.ServeHTTP(w, r)
	})}
	controlSrv := &http.Server{Addr: ":8081", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/state" && r.Method == http.MethodGet {
			c := s.active()
			s.mu.Lock()
			end := s.until
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"mode": c.Mode, "timeline_end": end, "upstream_seen": s.seen.Load(), "downstream_delivered": s.delivered.Load(), "dropped": s.dropped.Load()})
			return
		}
		if r.URL.Path != "/control" || r.Method != http.MethodPost {
			http.Error(w, "not found", 404)
			return
		}
		var c control
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF || validate(c) != nil {
			http.Error(w, "invalid control", 400)
			return
		}
		if c.Mode == "normal" {
			_, _ = exec.Command("tc", "qdisc", "del", "dev", "eth0", "root").CombinedOutput()
		} else if c.Mode == "delay" || c.Mode == "loss" {
			args := []string{"qdisc", "replace", "dev", "eth0", "root", "netem"}
			if c.Mode == "delay" {
				args = append(args, "delay", fmt.Sprintf("%dms", c.DelayMS))
			} else {
				args = append(args, "loss", "2%")
			}
			if out, err := exec.Command("tc", args...).CombinedOutput(); err != nil {
				http.Error(w, "netem capability unavailable: "+strings.TrimSpace(string(out))+": "+err.Error(), 503)
				return
			}
		}
		s.mu.Lock()
		s.cfg = c
		s.mode = c.Mode
		s.until = time.Time{}
		if c.DurationMS > 0 {
			s.until = time.Now().Add(time.Duration(c.DurationMS) * time.Millisecond)
		}
		s.mu.Unlock()
		if c.Mode == "cut-streams" || c.Mode == "offline" {
			s.cutStreams()
		}
		w.WriteHeader(204)
	})}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go httpSrv.ListenAndServe()
	go controlSrv.ListenAndServe()
	<-ctx.Done()
	_, _ = exec.Command("tc", "qdisc", "del", "dev", "eth0", "root").CombinedOutput()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdown)
	_ = controlSrv.Shutdown(shutdown)
}
func validate(c control) error {
	switch c.Mode {
	case "normal":
		if c.DurationMS != 0 || c.DelayMS != 0 || c.DropCount != 0 || c.RequestID != "" || c.EventType != "" {
			return errors.New("fields")
		}
	case "hold-refresh":
		if c.DelayMS < 1 || c.DelayMS > 500 || c.DurationMS != 0 || c.DropCount != 0 || c.RequestID != "" || c.EventType != "" {
			return errors.New("hold")
		}
	case "fail-refresh":
		if c.DurationMS < 1 || c.DurationMS > 120000 || c.DelayMS != 0 || c.DropCount != 0 || c.RequestID != "" || c.EventType != "" {
			return errors.New("duration")
		}
	case "cut-streams", "offline":
		if c.DurationMS < 1 || c.DurationMS > 120000 || c.DelayMS != 0 || c.DropCount != 0 || c.RequestID != "" || c.EventType != "" {
			return errors.New("duration")
		}
	case "drop-sse":
		if c.DropCount < 1 || c.DropCount > 1000 || c.RequestID == "" || (c.EventType != "GameRequestJoined" && c.EventType != "GameRequestLeft" && c.EventType != "game-request-chat") || c.DurationMS != 0 || c.DelayMS != 0 {
			return errors.New("drop")
		}
	case "delay":
		if c.DurationMS < 1 || c.DurationMS > 120000 || (c.DelayMS != 200 && c.DelayMS != 500) || c.DropCount != 0 || c.RequestID != "" || c.EventType != "" {
			return errors.New("delay")
		}
	case "loss":
		if c.DurationMS < 1 || c.DurationMS > 120000 || c.DelayMS != 0 || c.DropCount != 0 || c.RequestID != "" || c.EventType != "" {
			return errors.New("loss")
		}
	default:
		return errors.New("mode")
	}
	return nil
}

type frameFilter struct {
	r       io.ReadCloser
	scanner *bufio.Reader
	cfg     control
	s       *state
	pending []byte
	dropped int
}

func newFrameFilter(r io.ReadCloser, c control, s *state) *frameFilter {
	return &frameFilter{r: r, scanner: bufio.NewReader(r), cfg: c, s: s}
}
func (f *frameFilter) Close() error { return f.r.Close() }
func (f *frameFilter) Read(p []byte) (int, error) {
	for len(f.pending) == 0 {
		frame, err := readFrame(f.scanner)
		if len(frame) > 0 {
			f.s.seen.Add(1)
			var id, typ string
			for _, line := range strings.Split(string(frame), "\n") {
				if strings.HasPrefix(line, "event:") {
					typ = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				}
				if strings.HasPrefix(line, "data:") {
					var v struct {
						RequestID string `json:"requestId"`
						Type      string `json:"type"`
					}
					_ = json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &v)
					id = v.RequestID
					if typ == "" || typ == "game-request" {
						typ = v.Type
					}
				}
			}
			if id == f.cfg.RequestID && typ == f.cfg.EventType && f.dropped < f.cfg.DropCount {
				f.dropped++
				f.s.dropped.Add(1)
			} else {
				f.pending = append(f.pending, frame...)
				f.s.delivered.Add(1)
			}
		}
		if err != nil && len(f.pending) == 0 {
			return 0, err
		}
	}
	n := copy(p, f.pending)
	f.pending = f.pending[n:]
	return n, nil
}
func readFrame(r *bufio.Reader) ([]byte, error) {
	var b []byte
	for {
		line, err := r.ReadBytes('\n')
		b = append(b, line...)
		if err != nil {
			return b, err
		}
		if len(line) == 1 && line[0] == '\n' {
			return b, nil
		}
	}
}

type httptestRecorder struct {
	header http.Header
	status int
	body   strings.Builder
}

func (r *httptestRecorder) Header() http.Header { return r.header }
func (r *httptestRecorder) WriteHeader(n int)   { r.status = n }
func (r *httptestRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	return r.body.Write(p)
}
