package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"
)

func TestSelectSessionsAllowsTriggerToReuseExistingSession(t *testing.T) {
	tokens := make([]string, 400)
	for i := range tokens {
		tokens[i] = fmt.Sprint(i + 1)
	}
	selected, trigger, err := selectSessions(tokens, 0, 400, true)
	if err != nil || len(selected) != 400 || selected[399] != trigger {
		t.Fatalf("selected=%d trigger=%q err=%v, want last client session as trigger", len(selected), trigger, err)
	}
}

func TestFanoutMeasuresFromCompletedPost(t *testing.T) {
	base := time.Unix(100, 0)
	for _, tc := range []struct {
		name  string
		early bool
		want  time.Duration
	}{
		{name: "event arrives before response", early: true, want: -time.Second},
		{name: "event arrives after response", want: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFanout("request", 1, time.Millisecond, "off", http.DefaultClient)
			state := &clientState{received: make([]time.Time, 1)}
			f.register(state)
			eventAt := base.Add(time.Second)
			completedAt := base.Add(2 * time.Second)
			if tc.early {
				f.observe(state, eventData{Type: "GameRequestJoined", RequestID: "request"}, eventAt)
				f.completeMutation(0, completedAt)
			} else {
				f.completeMutation(0, eventAt)
				f.observe(state, eventData{Type: "GameRequestJoined", RequestID: "request"}, completedAt)
			}
			if len(f.delays) != 1 || f.delays[0] != tc.want {
				t.Fatalf("delays = %v, want [%v]", f.delays, tc.want)
			}
		})
	}
}
func TestReadSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	input := "{\"session_token\":\"abc\"}\n{\"session_token\":\"xyz\"}\n"
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	tokens, err := readSessions(path)
	if err != nil || len(tokens) != 2 || tokens[0] != "abc" || tokens[1] != "xyz" {
		t.Fatalf("got %q, error %v", tokens, err)
	}
}

func TestReadSessionsRejectsMissingToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	if err := os.WriteFile(path, []byte("{\"user_id\":\"test\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessions(path); err == nil {
		t.Fatal("expected error on missing session token")
	}
}

func TestConsumeEventsCountsNamedGameRequestEvents(t *testing.T) {
	var stats counters
	input := ": connected\n\nevent: game-request\ndata: {}\n\nevent: game-request-chat\ndata: {}\n\nevent: other\ndata: foo\n\n"
	_ = consumeEvents(strings.NewReader(input), &stats, nil)
	if stats.events.Load() != 2 {
		t.Fatalf("got %d, want 2 named event frames", stats.events.Load())
	}
}

func TestPercentile(t *testing.T) {
	ms := func(values ...int) []time.Duration {
		out := make([]time.Duration, len(values))
		for i, v := range values {
			out[i] = time.Duration(v) * time.Millisecond
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		values []time.Duration
		p      float64
		want   time.Duration
	}{
		{"empty", nil, 95, 0},
		{"single", ms(7), 99, 7 * time.Millisecond},
		{"unsorted input", ms(30, 10, 20, 40), 50, 20 * time.Millisecond},
		{"p95 of 100", ms(seq(100)...), 95, 95 * time.Millisecond},
		{"max", ms(5, 1, 9), 100, 9 * time.Millisecond},
	} {
		if got := percentile(tc.values, tc.p); got != tc.want {
			t.Errorf("%s: percentile(p%v) = %v, want %v", tc.name, tc.p, got, tc.want)
		}
	}
}

func seq(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

func TestConsumeEventsReportsNamedGameRequestData(t *testing.T) {
	var stats counters
	var got []eventData
	input := "event: game-request\ndata: {\"type\":\"GameRequestJoined\",\"requestId\":\"r1\"}\n\n" +
		"event: game-request-chat\ndata: {\"type\":\"GameRequestChatMessage\",\"requestId\":\"r1\"}\n\n" +
		"event: game-request\ndata: not-json\n\n"
	_ = consumeEvents(strings.NewReader(input), &stats, func(e eventData) { got = append(got, e) })
	if len(got) != 2 || got[0].Type != "GameRequestJoined" || got[0].RequestID != "r1" || got[1].Type != "GameRequestChatMessage" {
		t.Fatalf("observed %+v, want valid list and chat events", got)
	}
	if stats.events.Load() != 3 || stats.malformed.Load() != 1 {
		t.Fatalf("events=%d malformed=%d, want 3 and 1", stats.events.Load(), stats.malformed.Load())
	}
}

func TestBrowserLikeRefreshCoalescesEventsWhileRequestActive(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			entered <- struct{}{}
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	f := newFanout("", 0, time.Millisecond, "browser-like", server.Client())
	f.baseURL = server.URL
	state := &clientState{token: "test"}
	f.observe(state, eventData{}, time.Now())
	<-entered
	for range 40 {
		f.observe(state, eventData{}, time.Now())
	}
	close(release)
	f.wg.Wait()
	if got := requests.Load(); got != 2 {
		t.Fatalf("refresh requests = %d, want first plus one trailing refresh", got)
	}
}

func TestWorstCaseRefreshStartsOneRequestPerEvent(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	f := newFanout("", 0, time.Millisecond, "worst-case", server.Client())
	f.baseURL = server.URL
	state := &clientState{token: "test"}
	for range 40 {
		f.observe(state, eventData{}, time.Now())
	}
	f.wg.Wait()
	if got := requests.Load(); got != 40 {
		t.Fatalf("refresh requests = %d, want 40", got)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRefreshCountsTruncatedOKBodyAsFailure(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))}, nil
	})}
	f := newFanout("", 0, time.Millisecond, "worst-case", client)
	f.baseURL = "http://loadtest.invalid"
	f.observe(&clientState{token: "test"}, eventData{}, time.Now())
	if _, failures := f.report(1); failures != 1 || f.refreshes2xx != 0 {
		t.Fatalf("truncated 200: failures=%d ok=%d, want 1 failure and 0 ok", failures, f.refreshes2xx)
	}
}
func TestCoalescedRefreshWaitsForFixedWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int64
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		f := newFanout("", 0, time.Millisecond, "coalesced", client)
		f.baseURL = "http://loadtest.invalid"
		state := &clientState{token: "test"}
		f.observe(state, eventData{streamName: "game-request"}, time.Now())
		synctest.Wait()
		for range 2 {
			time.Sleep(50 * time.Millisecond)
			f.observe(state, eventData{streamName: "game-request"}, time.Now())
		}
		synctest.Wait()
		if got := requests.Load(); got != 0 {
			t.Fatalf("requests before 200 ms = %d, want 0", got)
		}
		f.observe(state, eventData{streamName: "game-request"}, time.Now())
		time.Sleep(101 * time.Millisecond)
		synctest.Wait()
		if got := requests.Load(); got != 1 {
			t.Fatalf("requests at 200 ms = %d, want 1", got)
		}
	})
}

func TestCoalescedRefreshDoesNotSlideUnderContinuousSignals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int64
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		f := newFanout("", 0, time.Millisecond, "coalesced", client)
		f.baseURL = "http://loadtest.invalid"
		state := &clientState{token: "test"}
		for i := range 20 {
			f.observe(state, eventData{streamName: "game-request"}, time.Now())
			if i == 0 {
				synctest.Wait()
			}
			time.Sleep(50 * time.Millisecond)
			synctest.Wait()
			if i == 3 && requests.Load() == 0 {
				t.Fatal("continuous signals postponed first refresh beyond 200 ms")
			}
		}
		f.wg.Wait()
		if got := requests.Load(); got >= 20 {
			t.Fatalf("requests under continuous signals = %d, want coalesced work", got)
		}
	})
}

func TestCoalescedRefreshQueuesOneRequestBehindHeldResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		var requests atomic.Int64
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			if requests.Add(1) == 1 {
				entered <- struct{}{}
				<-release
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		f := newFanout("", 0, time.Millisecond, "coalesced", client)
		f.baseURL = "http://loadtest.invalid"
		state := &clientState{token: "test"}
		f.observe(state, eventData{streamName: "game-request"}, time.Now())
		synctest.Wait()
		time.Sleep(201 * time.Millisecond)
		<-entered
		for range 20 {
			f.observe(state, eventData{streamName: "game-request"}, time.Now())
			time.Sleep(50 * time.Millisecond)
		}
		got := requests.Load()
		close(release)
		f.wg.Wait()
		if got != 1 {
			t.Fatalf("requests while first response held = %d, want 1", got)
		}
		if final := requests.Load(); final != 2 {
			t.Fatalf("requests after held response = %d, want one trailing request", final)
		}
	})
}

func TestCoalescedRefreshRetriesOnceAfterOneSecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int64
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		f := newFanout("", 0, time.Millisecond, "coalesced", client)
		f.baseURL = "http://loadtest.invalid"
		f.observe(&clientState{token: "test"}, eventData{streamName: "game-request"}, time.Now())
		synctest.Wait()
		time.Sleep(201 * time.Millisecond)
		synctest.Wait()
		if got := requests.Load(); got != 1 {
			t.Fatalf("requests before retry delay = %d, want 1", got)
		}
		time.Sleep(998 * time.Millisecond)
		synctest.Wait()
		if got := requests.Load(); got != 1 {
			t.Fatalf("requests at 999 ms retry delay = %d, want 1", got)
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if got := requests.Load(); got != 2 {
			t.Fatalf("requests at 1000 ms retry delay = %d, want 2", got)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if got := requests.Load(); got != 2 {
			t.Fatalf("requests after failed retry = %d, want no third request", got)
		}
	})
}
func TestCoalescedRefreshIgnoresChatAndActivityFrames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int64
		client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		f := newFanout("r1", 1, time.Millisecond, "coalesced", client)
		f.baseURL = "http://loadtest.invalid"
		state := &clientState{token: "test"}
		f.register(state)
		var stats counters
		frames := "event: game-request\ndata: {\"type\":\"GameRequestJoined\",\"requestId\":\"r1\"}\n\n" +
			"event: game-request-chat\ndata: {\"type\":\"GameRequestChatMessage\",\"requestId\":\"r1\"}\n\n" +
			"event: game-request-activity\ndata: {\"type\":\"GameRequestActivity\",\"requestId\":\"r1\"}\n\n"
		if err := consumeEvents(strings.NewReader(frames), &stats, func(event eventData) {
			f.observe(state, event, time.Now())
		}); err != io.EOF {
			t.Fatalf("consumeEvents error = %v, want EOF", err)
		}
		f.wg.Wait()
		if got := requests.Load(); got != 1 {
			t.Fatalf("refresh requests = %d, want only game-request frame", got)
		}
		if f.refreshSignals != 1 {
			t.Fatalf("refresh signals = %d, want 1", f.refreshSignals)
		}
	})
}
