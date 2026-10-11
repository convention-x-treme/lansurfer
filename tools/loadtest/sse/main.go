// Command sse opens authenticated LANsurfer SSE connections on the isolated
// localhost-only load-test instance. No external identity provider is involved.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/convention-x-treme/lansurfer/tools/loadtest/internal/target"
)

const directOrigin = "http://127.0.0.1:18080"

type counters struct {
	attempts     atomic.Int64
	connected    atomic.Int64
	active       atomic.Int64
	peak         atomic.Int64
	events       atomic.Int64
	disconnects  atomic.Int64
	failures     atomic.Int64
	malformed    atomic.Int64
	unassignable atomic.Int64
}

func main() {
	clients := flag.Int("clients", 20, "number of concurrent SSE streams (1-2000)")
	duration := flag.Duration("duration", time.Minute, "time for which streams stay open")
	sessions := flag.String("sessions", "", "path to local test sessions JSONL")
	request := flag.String("request", "", "open Game Request ID for the mutation fan-out measurement")
	trigger := flag.Int("trigger", 0, "number of join/leave mutations (needs -request)")
	triggerMode := flag.String("trigger-mode", "join-leave", "trigger action: join-leave or chat")
	triggerDelay := flag.Duration("trigger-delay", 0, "delay after all streams connect before mutations")

	interval := flag.Duration("interval", 500*time.Millisecond, "spacing between mutations")
	refreshMode := flag.String("refresh-mode", "off", "refresh behavior: off, browser-like, worst-case")
	baseURL := flag.String("base-url", directOrigin, "allowed isolated HTTP origin")
	offset := flag.Int("session-offset", 0, "first session row")
	connectMode := flag.String("connect-mode", "staggered", "connection startup: staggered or barrier")
	allowReconnect := flag.Bool("allow-reconnect", false, "tolerate drops and report recovery")
	allowMissing := flag.Bool("allow-missing-events", false, "report but tolerate expected stream events lost during network faults")
	refreshQueryFlag := flag.String("refresh-query", "", "optional encoded query for refresh GETs, for example q=Warcraft+III")
	flag.Parse()
	if *refreshQueryFlag != "" {
		for _, c := range *refreshQueryFlag {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("=&+%-_.", c)) {
				fmt.Fprintln(os.Stderr, "SSE load test: -refresh-query contains unsupported characters")
				os.Exit(2)
			}
		}
		refreshQuery = "?" + *refreshQueryFlag
	}
	if err := run(options{
		clients: *clients, duration: *duration, sessionsPath: *sessions, request: *request,
		trigger: *trigger, triggerDelay: *triggerDelay, interval: *interval, refreshMode: *refreshMode, baseURL: *baseURL,
		sessionOffset: *offset, allowReconnect: *allowReconnect, allowMissingEvents: *allowMissing, connectMode: *connectMode, triggerMode: *triggerMode,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "SSE load test:", err)
		os.Exit(1)
	}
}

type options struct {
	clients            int
	duration           time.Duration
	sessionsPath       string
	request            string
	trigger            int
	triggerDelay       time.Duration
	interval           time.Duration
	refreshMode        string
	baseURL            string
	sessionOffset      int
	allowReconnect     bool
	allowMissingEvents bool
	connectMode        string
	triggerMode        string
}

func selectSessions(tokens []string, offset, clients int, needsTrigger bool) ([]string, string, error) {
	if offset < 0 || clients < 1 || offset+clients > len(tokens) {
		return nil, "", fmt.Errorf("only %d sessions provided for offset %d and %d clients", len(tokens), offset, clients)
	}
	trigger := ""
	if needsTrigger {
		if len(tokens) == 0 {
			return nil, "", errors.New("no session available for mutation trigger")
		}
		trigger = tokens[len(tokens)-1]
	}
	return tokens[offset : offset+clients], trigger, nil
}

func run(o options) error {
	if o.clients < 1 || o.clients > 2000 || o.duration <= 0 {
		return errors.New("-clients must be 1..2000 and -duration must be positive")
	}
	if o.triggerMode != "join-leave" && o.triggerMode != "chat" {
		return errors.New("-trigger-mode must be join-leave or chat")
	}
	if o.triggerMode == "chat" && (o.trigger != 1 || o.request == "") {
		return errors.New("chat trigger requires -trigger 1 and -request")
	}
	if o.refreshMode != "off" && o.refreshMode != "browser-like" && o.refreshMode != "worst-case" {
		return errors.New("-refresh-mode must be off, browser-like or worst-case")
	}
	if o.connectMode != "staggered" && o.connectMode != "barrier" {
		return errors.New("-connect-mode must be staggered or barrier")
	}
	if err := target.ValidateHTTPURL(o.baseURL); err != nil {
		return errors.New("invalid isolated HTTP target")
	}
	if o.sessionOffset < 0 {
		return errors.New("-session-offset must be non-negative")
	}
	if o.sessionsPath == "" {
		return errors.New("specify -sessions pointing to the local seed output")
	}
	if o.trigger < 0 || (o.trigger > 0 && o.request == "") {
		return errors.New("-trigger must be non-negative and needs -request")
	}
	if o.triggerDelay < 0 || o.triggerDelay > 30*time.Minute || (o.trigger > 0 && o.triggerDelay >= o.duration) {
		return errors.New("-trigger-delay must be non-negative, at most 30m, and shorter than duration")
	}
	tokens, err := readSessions(o.sessionsPath)
	if err != nil {
		return err
	}
	selected, triggerToken, err := selectSessions(tokens, o.sessionOffset, o.clients, o.trigger > 0)
	if err != nil {
		return err
	}
	if o.triggerMode == "chat" {
		if len(tokens) < 2 {
			return errors.New("chat trigger needs reserved control-host session")
		}
		triggerToken = tokens[len(tokens)-2]
	}
	tokens = selected
	endpoint := strings.TrimRight(o.baseURL, "/") + "/events/game-requests"

	ctxSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctxSignal, o.duration)
	defer cancel()
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          o.clients,
		MaxIdleConnsPerHost:   o.clients,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{
		Transport:     transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	var fan *fanout
	if o.trigger > 0 || o.refreshMode != "off" {
		fan = newFanout(o.request, o.trigger, o.interval, o.refreshMode, httpClient)
		fan.chat = o.triggerMode == "chat"
		fan.baseURL = strings.TrimRight(o.baseURL, "/")
	}
	var totals counters
	var wg sync.WaitGroup
	fmt.Printf("Opening %d authenticated SSE streams for up to %s (%s)\n", o.clients, o.duration, endpoint)

	// Recovery: time from the first stream drop until every stream is back.
	var recoveries []time.Duration
	var downSince time.Time
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C:
				active := totals.active.Load()
				if totals.peak.Load() >= int64(o.clients) && active < int64(o.clients) && downSince.IsZero() {
					downSince = now
				} else if active >= int64(o.clients) && !downSince.IsZero() {
					recoveries = append(recoveries, now.Sub(downSince))
					downSince = time.Time{}
				}
			}
		}
	}()

	triggerResult := make(chan error, 1)
	if o.trigger > 0 {
		go func() {
			for totals.active.Load() < int64(o.clients) {
				select {
				case <-ctx.Done():
					triggerResult <- ctx.Err()
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
			if o.triggerDelay > 0 {
				timer := time.NewTimer(o.triggerDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					triggerResult <- ctx.Err()
					return
				case <-timer.C:
				}
			}
			err := fan.run(ctx, triggerToken)
			triggerResult <- err
		}()
	}

	for i := 0; i < o.clients && ctx.Err() == nil; i++ {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			watch(ctx, httpClient, endpoint, token, &totals, fan)
		}(tokens[i])
		if o.connectMode == "staggered" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	<-ctx.Done()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 20*time.Second)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-drainCtx.Done():
	}
	drainCancel()
	<-monitorDone
	fmt.Printf("SSE result: attempts=%d connected=%d peak_active=%d/%d events=%d malformed=%d unassignable=%d unexpected_disconnects=%d connect_failures=%d\n",
		totals.attempts.Load(), totals.connected.Load(), totals.peak.Load(), o.clients,
		totals.events.Load(), totals.malformed.Load(), totals.unassignable.Load(), totals.disconnects.Load(), totals.failures.Load())
	if len(recoveries) > 0 || !downSince.IsZero() {
		fmt.Printf("SSE recovery: outages=%d recovery_times=%v still_down_at_end=%t\n", len(recoveries), recoveries, !downSince.IsZero())
	}
	if ctxSignal.Err() != nil {
		return errors.New("interrupted before test completed")
	}
	if fan != nil {
		missing, refreshFailures := fan.report(o.clients)
		if o.trigger > 0 && missing > 0 && !o.allowMissingEvents {
			return fmt.Errorf("%d expected stream deliveries were missing", missing)
		}
		if refreshFailures > 0 {
			return fmt.Errorf("%d refresh requests failed", refreshFailures)
		}
	}
	select {
	case err := <-triggerResult:
		if err != nil {
			return fmt.Errorf("mutation trigger: %w", err)
		}
	default:
	}
	if totals.peak.Load() < int64(o.clients) {
		return errors.New("not every SSE stream connected")
	}
	if o.allowReconnect {
		if !downSince.IsZero() {
			return errors.New("SSE streams did not all reconnect before the test ended")
		}
		return nil
	}
	if totals.failures.Load() != 0 || totals.disconnects.Load() != 0 {
		return errors.New("SSE connections failed, dropped or did not all stay connected")
	}
	return nil
}

func readSessions(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open credentials: %w", err)
	}
	defer file.Close()
	var tokens []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, errors.New("invalid JSONL credentials")
		}
		token, valid := row["session_token"].(string)
		if !valid || token == "" {
			return nil, errors.New("invalid JSONL session token")
		}
		tokens = append(tokens, token)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	return tokens, nil
}

func watch(ctx context.Context, client *http.Client, endpoint string, token string, stats *counters, fan *fanout) {
	state := &clientState{token: token}
	if fan != nil {
		fan.register(state)
	}
	var observe func(eventData)
	if fan != nil {
		observe = func(e eventData) { fan.observe(state, e, time.Now()) }
	}
	for ctx.Err() == nil {
		stats.attempts.Add(1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			stats.failures.Add(1)
			return
		}
		req.AddCookie(&http.Cookie{Name: "lansurfer_session", Value: token})
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
		response, err := client.Do(req)
		if ctx.Err() != nil {
			if response != nil {
				_ = response.Body.Close()
			}
			return
		}
		if err != nil {
			if stats.failures.Add(1) <= 3 {
				fmt.Fprintln(os.Stderr, "SSE connection error:", err)
			}
		} else if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			if stats.failures.Add(1) <= 3 {
				fmt.Fprintln(os.Stderr, "SSE returned unexpected HTTP status:", response.StatusCode)
			}
			_ = response.Body.Close()
		} else {
			stats.connected.Add(1)
			active := stats.active.Add(1)
			for {
				peak := stats.peak.Load()
				if active <= peak || stats.peak.CompareAndSwap(peak, active) {
					break
				}
			}
			err = consumeEvents(response.Body, stats, observe)
			_ = response.Body.Close()
			stats.active.Add(-1)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				stats.disconnects.Add(1)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func consumeEvents(reader io.Reader, stats *counters, observe func(eventData)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	eventName, data := "", ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if eventName == "game-request" || eventName == "game-request-chat" || eventName == "game-request-activity" {
				stats.events.Add(1)
				var event eventData
				if json.Unmarshal([]byte(data), &event) != nil {
					stats.malformed.Add(1)
				} else if observe != nil && event.RequestID == "" {
					stats.unassignable.Add(1)
				} else if observe != nil {
					observe(event)
				}
			}
			eventName, data = "", ""
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF // Closing the stream before the deadline is unexpected.
}
