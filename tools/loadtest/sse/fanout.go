package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

// refreshQuery is an optional validated query appended to synthetic refresh GETs.
var refreshQuery string

// eventData is the JSON payload of a Game Request SSE event.
type eventData struct {
	Type       string `json:"type"`
	RequestID  string `json:"requestId"`
	streamName string
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
	request     string
	mutation    int
	interval    time.Duration
	refreshMode string
	baseURL     string
	client      *http.Client
	sentAt      []atomic.Int64
	chat        bool

	mu              sync.Mutex
	clients         []*clientState
	delays          []time.Duration
	refreshes       []time.Duration
	postLatencies   []time.Duration
	outOfOrder      int
	observed        int
	unassignable    int
	duplicates      int
	refreshFailure  int
	refreshTimeouts int
	refreshes2xx    int
	refreshActive   int
	refreshPeak     int
	refreshSignals  int
	gameRequestOnly bool
	wg              sync.WaitGroup
}

func newFanout(request string, mutations int, interval time.Duration, refreshMode string, client *http.Client) *fanout {
	return &fanout{request: request, mutation: mutations, interval: interval, refreshMode: refreshMode, baseURL: baseURL, client: client, sentAt: make([]atomic.Int64, mutations)}
}

// clientState counts target-request events seen by one player, so the k-th
// event can be matched to the k-th mutation (join, leave, join, ...).
type clientState struct {
	token          string
	seen           int
	mappingGap     bool
	received       []time.Time
	refreshMu      sync.Mutex
	refreshActive  bool
	refreshQueued  bool
	refreshPending bool
	refreshDue     time.Time
}

func (f *fanout) register(state *clientState) {
	if len(state.received) != f.mutation {
		state.received = make([]time.Time, f.mutation)
	}
	f.mu.Lock()
	f.clients = append(f.clients, state)
	f.mu.Unlock()
}

func (f *fanout) observe(state *clientState, event eventData, at time.Time) {
	gameRequest := event.streamName == "game-request"
	if f.refreshMode != "off" && ((!f.gameRequestOnly && f.refreshMode != "coalesced") || gameRequest) {
		f.mu.Lock()
		f.refreshSignals++
		f.mu.Unlock()
		switch f.refreshMode {
		case "browser-like":
			state.refreshMu.Lock()
			if state.refreshActive {
				state.refreshQueued = true
				state.refreshMu.Unlock()
				break
			}
			state.refreshActive = true
			state.refreshMu.Unlock()
			f.wg.Add(1)
			go f.refreshBrowserLike(state)
		case "coalesced":
			f.refreshCoalesced(state)
		default:
			f.wg.Add(1)
			go func() { defer f.wg.Done(); f.refreshList(state.token) }()
		}
	}
	if (f.gameRequestOnly || f.refreshMode == "coalesced") && !gameRequest {
		return
	}
	if event.RequestID != f.request {
		return
	}
	if event.Type != "GameRequestJoined" && event.Type != "GameRequestLeft" {
		f.mu.Lock()
		f.unassignable++
		f.mu.Unlock()
		state.mappingGap = true
		return
	}
	if state.seen >= f.mutation {
		f.mu.Lock()
		f.duplicates++
		f.mu.Unlock()
		return
	}
	k := state.seen
	state.seen++
	wantType := "GameRequestJoined"
	if k%2 == 1 {
		wantType = "GameRequestLeft"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observed++
	if event.Type != wantType {
		f.outOfOrder++
		state.mappingGap = true
		return
	}
	if state.mappingGap {
		return
	}
	state.received[k] = at
	if sent := f.sentAt[k].Load(); sent != 0 {
		f.delays = append(f.delays, at.Sub(time.Unix(0, sent)))
		state.received[k] = time.Time{}
	}
}

func (f *fanout) refreshCoalesced(state *clientState) {
	state.refreshMu.Lock()
	if state.refreshPending {
		state.refreshMu.Unlock()
		return
	}
	state.refreshPending = true
	state.refreshDue = time.Now().Add(100 * time.Millisecond)
	if state.refreshActive {
		state.refreshMu.Unlock()
		return
	}
	state.refreshActive = true
	f.wg.Add(1)
	state.refreshMu.Unlock()
	go f.runCoalescedRefresh(state)
}

func (f *fanout) runCoalescedRefresh(state *clientState) {
	defer f.wg.Done()
	retryUsed := false
	for {
		state.refreshMu.Lock()
		due := state.refreshDue
		state.refreshMu.Unlock()
		if delay := time.Until(due); delay > 0 {
			time.Sleep(delay)
		}
		state.refreshMu.Lock()
		state.refreshPending = false
		state.refreshMu.Unlock()
		ok := f.refreshList(state.token)
		if !ok && !retryUsed {
			retryUsed = true
			time.Sleep(time.Second)
			state.refreshMu.Lock()
			state.refreshPending = false
			state.refreshMu.Unlock()
			ok = f.refreshList(state.token)
		}
		state.refreshMu.Lock()
		pending := state.refreshPending
		if ok {
			retryUsed = false
		} else if retryUsed {
			retryUsed = false
		}
		if !pending {
			state.refreshActive = false
			state.refreshMu.Unlock()
			return
		}
		state.refreshMu.Unlock()
	}
}

func (f *fanout) completeMutation(k int, completed time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentAt[k].Store(completed.UnixNano())
	for _, state := range f.clients {
		if !state.received[k].IsZero() {
			f.delays = append(f.delays, state.received[k].Sub(completed))
			state.received[k] = time.Time{}
		}
	}
}

func (f *fanout) refreshBrowserLike(state *clientState) {
	defer f.wg.Done()
	for {
		f.refreshList(state.token)
		state.refreshMu.Lock()
		if !state.refreshQueued {
			state.refreshActive = false
			state.refreshMu.Unlock()
			return
		}
		state.refreshQueued = false
		state.refreshMu.Unlock()
	}
}
func (f *fanout) refreshList(token string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+refreshPath+refreshQuery, nil)
	if err != nil {
		return false
	}
	req.AddCookie(&http.Cookie{Name: "lansurfer_session", Value: token})
	req.Header.Set("HX-Request", "true")
	start := time.Now()
	f.mu.Lock()
	f.refreshActive++
	if f.refreshActive > f.refreshPeak {
		f.refreshPeak = f.refreshActive
	}
	f.mu.Unlock()
	resp, err := f.client.Do(req)
	if err == nil {
		// A 200 header does not prove the fragment arrived: a connection dropped
		// mid-body is a failed refresh.
		_, err = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	elapsed := time.Since(start)
	f.mu.Lock()
	f.refreshActive--
	defer f.mu.Unlock()
	if err != nil || resp.StatusCode != http.StatusOK {
		if err != nil {
			var timeout net.Error
			if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
				f.refreshTimeouts++
			}
		}
		f.refreshFailure++
		return false
	}
	f.refreshes2xx++
	f.refreshes = append(f.refreshes, elapsed)
	return true
}

// run performs the mutations as one real player with a real CSRF token. It
// returns after the last mutation; the caller waits for delivery to settle.
func (f *fanout) run(ctx context.Context, token string) error {
	target, _ := url.Parse(f.baseURL)
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(target, []*http.Cookie{{Name: "lansurfer_session", Value: token, Path: "/"}})
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(f.baseURL + "/game-requests/new")
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
		if f.chat {
			action = "chat"
			form = url.Values{"csrf_token": {csrf}, "body": {"loadtest phase2 lost-signal marker"}}
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, f.baseURL+"/game-requests/"+f.request+"/"+action, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("%s: %w", action, err)
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("HX-Retarget") != "" {
			_ = resp.Body.Close()
			return fmt.Errorf("%s was not accepted (status %d); pick an open request with a free place that the trigger player does not host", action, resp.StatusCode)
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			_ = resp.Body.Close()
			return fmt.Errorf("read %s response: %w", action, err)
		}
		_ = resp.Body.Close()
		f.completeMutation(k, time.Now())
		f.mu.Lock()
		f.postLatencies = append(f.postLatencies, time.Since(start))
		f.mu.Unlock()
	}
	return nil
}

// report prints aggregate numbers and returns missing expected deliveries and refresh failures.
func (f *fanout) report(clients int) (missing, refreshFailures int) {
	f.wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	expected := clients * f.mutation
	missing = expected - f.observed
	refreshFailures = f.refreshFailure
	fmt.Fprintf(os.Stdout, "fan-out: mutations=%d streams=%d expected_deliveries=%d observed=%d out_of_order=%d unassignable=%d duplicate=%d missing=%d\n",
		f.mutation, clients, expected, f.observed, f.outOfOrder, f.unassignable, f.duplicates, missing)
	if missing == 0 && f.outOfOrder == 0 && f.unassignable == 0 {
		fmt.Fprintf(os.Stdout, "fan-out: mutation->stream delay p50=%v p95=%v p99=%v max=%v (measured after the completed POST response)\n",
			percentile(f.delays, 50), percentile(f.delays, 95), percentile(f.delays, 99), percentile(f.delays, 100))
	} else {
		fmt.Fprintln(os.Stdout, "fan-out: mutation->stream latency unavailable because delivery mapping is incomplete")
	}
	fmt.Fprintf(os.Stdout, "fan-out: mutation POST latency p50=%v p95=%v p99=%v max=%v\n",
		percentile(f.postLatencies, 50), percentile(f.postLatencies, 95), percentile(f.postLatencies, 99), percentile(f.postLatencies, 100))
	fmt.Fprintf(os.Stdout, "fan-out: game-request refresh signals=%d refreshes_per_signal=", f.refreshSignals)
	if f.refreshSignals == 0 {
		fmt.Fprintln(os.Stdout, "unavailable")
	} else {
		fmt.Fprintf(os.Stdout, "%.2f\n", float64(f.refreshes2xx+f.refreshFailure)/float64(f.refreshSignals))
	}
	if f.refreshMode != "off" {
		fmt.Fprintf(os.Stdout, "fan-out: refresh amplification GET %s=%d ok=%d failed=%d timeouts=%d peak_parallel=%d latency p50=%v p95=%v p99=%v max=%v\n",
			refreshPath+refreshQuery, f.refreshes2xx+f.refreshFailure, f.refreshes2xx, f.refreshFailure, f.refreshTimeouts, f.refreshPeak,
			percentile(f.refreshes, 50), percentile(f.refreshes, 95), percentile(f.refreshes, 99), percentile(f.refreshes, 100))
	}
	return missing, refreshFailures
}
