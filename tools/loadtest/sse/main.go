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
)

const endpoint = "http://127.0.0.1:18080/events/game-requests"

type counters struct {
	attempts    atomic.Int64
	connected   atomic.Int64
	active      atomic.Int64
	peak        atomic.Int64
	events      atomic.Int64
	disconnects atomic.Int64
	failures    atomic.Int64
}

func main() {
	clients := flag.Int("clients", 20, "number of concurrent SSE streams (1-2000)")
	duration := flag.Duration("duration", time.Minute, "time for which streams stay open")
	sessions := flag.String("sessions", "", "path to local test sessions JSONL")
	flag.Parse()
	if err := run(*clients, *duration, *sessions); err != nil {
		fmt.Fprintln(os.Stderr, "SSE load test:", err)
		os.Exit(1)
	}
}

func run(clients int, duration time.Duration, sessionsPath string) error {
	if clients < 1 || clients > 2000 || duration <= 0 {
		return errors.New("-clients must be 1..2000 and -duration must be positive")
	}
	if sessionsPath == "" {
		return errors.New("specify -sessions pointing to the local seed output")
	}
	tokens, err := readSessions(sessionsPath)
	if err != nil {
		return err
	}
	if len(tokens) < clients {
		return fmt.Errorf("only %d sessions provided for %d clients", len(tokens), clients)
	}

	ctxSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctxSignal, duration)
	defer cancel()
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          clients,
		MaxIdleConnsPerHost:   clients,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	var totals counters
	var wg sync.WaitGroup
	fmt.Printf("Opening %d authenticated SSE streams for %s (%s)\n", clients, duration, endpoint)
	for i := 0; i < clients && ctx.Err() == nil; i++ {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			watch(ctx, httpClient, token, &totals)
		}(tokens[i])
		// Briefly stagger startup; measure sustained streams, not only connect bursts.
		time.Sleep(10 * time.Millisecond)
	}
	<-ctx.Done()
	wg.Wait()
	fmt.Printf("SSE result: attempts=%d connected=%d peak_active=%d/%d events=%d unexpected_disconnects=%d connect_failures=%d\n",
		totals.attempts.Load(), totals.connected.Load(), totals.peak.Load(), clients,
		totals.events.Load(), totals.disconnects.Load(), totals.failures.Load())
	if ctxSignal.Err() != nil {
		return errors.New("interrupted before test completed")
	}
	if totals.peak.Load() < int64(clients) || totals.failures.Load() != 0 || totals.disconnects.Load() != 0 {
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

func watch(ctx context.Context, client *http.Client, token string, stats *counters) {
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
			err = consumeEvents(response.Body, stats)
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

func consumeEvents(reader io.Reader, stats *counters) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	eventName := ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if eventName == "game-request" {
				stats.events.Add(1)
			}
			eventName = ""
		} else if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF // Closing the stream before the deadline is unexpected.
}
