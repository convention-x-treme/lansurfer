package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestControlValidation(t *testing.T) {
	valid := []control{{Mode: "normal"}, {Mode: "hold-refresh", DelayMS: 500}, {Mode: "fail-refresh", DurationMS: 1000}, {Mode: "cut-streams", DurationMS: 1000}, {Mode: "offline", DurationMS: 1000}, {Mode: "drop-sse", DropCount: 1, RequestID: "id", EventType: "game-request-chat"}, {Mode: "delay", DurationMS: 1000, DelayMS: 200}, {Mode: "loss", DurationMS: 1000}}
	for _, c := range valid {
		if err := validate(c); err != nil {
			t.Errorf("validate(%+v): %v", c, err)
		}
	}
	invalid := []control{{Mode: "normal", DelayMS: 1}, {Mode: "delay", DurationMS: 1, DelayMS: 201}, {Mode: "loss", DurationMS: 120001}, {Mode: "drop-sse", DropCount: 1, RequestID: "id", EventType: "untrusted"}, {Mode: "offline"}}
	for _, c := range invalid {
		if err := validate(c); err == nil {
			t.Errorf("validate(%+v) succeeded", c)
		}
	}
}

func TestDropParserPreservesCommentsAndDropsOnlyTargetFrame(t *testing.T) {
	input := ": connected\n\nevent: game-request\ndata: {\"requestId\":\"target\",\"type\":\"GameRequestJoined\"}\n\nevent: game-request\ndata: {\"requestId\":\"other\",\"type\":\"GameRequestJoined\"}\n\n"
	s := &state{}
	f := newFrameFilter(io.NopCloser(strings.NewReader(input)), control{Mode: "drop-sse", DropCount: 1, RequestID: "target", EventType: "GameRequestJoined"}, s)
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "target") || !strings.Contains(string(got), ": connected") || !strings.Contains(string(got), "other") {
		t.Fatalf("unexpected filtered frames: %q", got)
	}
	if s.dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want 1", s.dropped.Load())
	}
}
func TestReverseProxyReusesConcurrentUpstreamConnections(t *testing.T) {
	const parallel = 64
	var upstreamConnections atomic.Int64
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			upstreamConnections.Add(1)
		}
	}
	upstream.Start()
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	reverseProxy := newReverseProxy(target)
	defer reverseProxy.Transport.(*http.Transport).CloseIdleConnections()
	proxy := httptest.NewServer(reverseProxy)
	defer proxy.Close()

	client := proxy.Client()
	for range 2 {
		start := make(chan struct{})
		var requests sync.WaitGroup
		requests.Add(parallel)
		for range parallel {
			go func() {
				defer requests.Done()
				<-start
				resp, err := client.Get(proxy.URL)
				if err != nil {
					t.Errorf("GET through proxy: %v", err)
					return
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent {
					t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
				}
			}()
		}
		close(start)
		requests.Wait()
	}

	got := upstreamConnections.Load()
	t.Logf("opened %d upstream connections for %d concurrent requests across two waves", got, 2*parallel)
	if got > parallel+4 {
		t.Fatalf("opened %d upstream connections; want at most %d", got, parallel+4)
	}
}
