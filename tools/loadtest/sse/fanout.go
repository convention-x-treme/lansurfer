package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	baseURL     = "http://127.0.0.1:18080"
	refreshPath = "/game-requests/refresh"
)

// eventData is the JSON payload of a "game-request" SSE event.
type eventData struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
}

// percentile returns the nearest-rank p-th percentile (0 < p <= 100) of values.
// It does not modify its input and returns 0 for an empty slice.
func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(float64(len(sorted))*p/100 + 0.999999999)
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// fanout measures mutation-to-stream delay for one Game Request and, when
// refresh is set, the HTMX list refresh each browser would issue per event.
type fanout struct {
	request  string
	mutation int           // number of join/leave mutations to perform
	interval time.Duration // spacing between mutations
	refresh  bool
	client   *http.Client
	sentAt   []atomic.Int64 // unix nanos, set immediately before mutation k is sent

	mu             sync.Mutex
	delays         []time.Duration
	refreshes      []time.Duration
	postLatencies  []time.Duration
	outOfOrder     int
	refreshFailure int
	refreshes2xx   int
	wg             sync.WaitGroup
}

func newFanout(request string, mutations int, interval time.Duration, refresh bool, client *http.Client) *fanout {
	return &fanout{request: request, mutation: mutations, interval: interval, refresh: refresh, client: client, sentAt: make([]atomic.Int64, mutations)}
}

// clientState counts target-request events seen by one player, so the k-th
// event can be matched to the k-th mutation (join, leave, join, ...).
type clientState struct {
	token string
	seen  int
}

func (f *fanout) observe(state *clientState, event eventData, at time.Time) {
	if f.refresh {
		f.wg.Add(1)
		go func() { // the browser refreshes immediately and asynchronously
			defer f.wg.Done()
			f.refreshList(state.token)
		}()
	}
	if event.RequestID != f.request || state.seen >= f.mutation {
		return
	}
	k := state.seen
	state.seen++
	wantType := "GameRequestJoined"
	if k%2 == 1 {
		wantType = "GameRequestLeft"
	}
	sent := f.sentAt[k].Load()
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.Type != wantType || sent == 0 {
		f.outOfOrder++
		return
	}
	f.delays = append(f.delays, time.Duration(at.UnixNano()-sent))
}

func (f *fanout) refreshList(token string) {
	req, err := http.NewRequest(http.MethodGet, baseURL+refreshPath, nil)
	if err != nil {
		return
	}
	req.AddCookie(&http.Cookie{Name: "lansurfer_session", Value: token})
	req.Header.Set("HX-Request", "true")
	start := time.Now()
	resp, err := f.client.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	elapsed := time.Since(start)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil || resp.StatusCode != http.StatusOK {
		f.refreshFailure++
		return
	}
	f.refreshes2xx++
	f.refreshes = append(f.refreshes, elapsed)
}

// run performs the mutations as one real player with a real CSRF token. It
// returns after the last mutation; the caller waits for delivery to settle.
func (f *fanout) run(ctx context.Context, token string) error {
	target, _ := url.Parse(baseURL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(target, []*http.Cookie{{Name: "lansurfer_session", Value: token, Path: "/"}})
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(baseURL + "/game-requests/") // a page GET makes LANsurfer issue the CSRF cookie
	if err != nil {
		return fmt.Errorf("load page for CSRF token: %w", err)
	}
	_ = resp.Body.Close()
	csrf := ""
	for _, c := range jar.Cookies(target) {
		if c.Name == "lansurfer_csrf" {
			csrf = c.Value
		}
	}
	if resp.StatusCode != http.StatusOK || csrf == "" {
		return fmt.Errorf("trigger player cannot obtain a CSRF token (status %d)", resp.StatusCode)
	}
	for k := range f.mutation {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.interval):
		}
		action := "join"
		if k%2 == 1 {
			action = "leave"
		}
		form := url.Values{"csrf_token": {csrf}, "return_to": {"list"}}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/game-requests/"+f.request+"/"+action, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		start := time.Now()
		f.sentAt[k].Store(start.UnixNano())
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("%s: %w", action, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("HX-Retarget") != "" {
			return fmt.Errorf("%s was not accepted (status %d); pick an open request with a free place that the trigger player does not host", action, resp.StatusCode)
		}
		f.mu.Lock()
		f.postLatencies = append(f.postLatencies, time.Since(start))
		f.mu.Unlock()
	}
	return nil
}

// report prints aggregate numbers only and returns the number of players that
// missed at least one expected event, derived from the delay count.
func (f *fanout) report(clients int) (missing int) {
	f.wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	expected := clients * f.mutation
	missing = expected - len(f.delays) - f.outOfOrder
	fmt.Fprintf(os.Stdout, "fan-out: mutations=%d streams=%d expected_deliveries=%d observed=%d out_of_order=%d missing=%d\n",
		f.mutation, clients, expected, len(f.delays), f.outOfOrder, missing)
	fmt.Fprintf(os.Stdout, "fan-out: mutation->stream delay p50=%v p95=%v p99=%v max=%v (measured from just before the POST; includes POST time)\n",
		percentile(f.delays, 50), percentile(f.delays, 95), percentile(f.delays, 99), percentile(f.delays, 100))
	fmt.Fprintf(os.Stdout, "fan-out: mutation POST latency p50=%v p95=%v max=%v\n",
		percentile(f.postLatencies, 50), percentile(f.postLatencies, 95), percentile(f.postLatencies, 100))
	if f.refresh {
		fmt.Fprintf(os.Stdout, "fan-out: refresh amplification GET %s=%d ok=%d failed=%d latency p50=%v p95=%v p99=%v max=%v\n",
			refreshPath, f.refreshes2xx+f.refreshFailure, f.refreshes2xx, f.refreshFailure,
			percentile(f.refreshes, 50), percentile(f.refreshes, 95), percentile(f.refreshes, 99), percentile(f.refreshes, 100))
	}
	return missing
}
