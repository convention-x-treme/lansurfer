package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestCollectSamplesContinuesAfterSampleFailure(t *testing.T) {
	var output bytes.Buffer
	calls := 0
	samples, failures, invalid, err := collectSamples(
		context.Background(), 40*time.Millisecond, 5*time.Millisecond, time.Now(),
		bufio.NewWriter(&output),
		func() (bool, error) {
			calls++
			if calls == 1 {
				return false, errors.New("metrics scrape failed")
			}
			return calls == 2, nil
		},
	)
	if err != nil {
		t.Fatalf("collectSamples() error = %v", err)
	}
	if calls < 2 || samples == 0 || failures != 1 || invalid != 1 {
		t.Fatalf("calls=%d samples=%d failures=%d invalid=%d; want continued sampling with one failure and one invalid window", calls, samples, failures, invalid)
	}
	var failure sampleFailure
	if err := json.NewDecoder(&output).Decode(&failure); err != nil {
		t.Fatalf("decode failure record: %v", err)
	}
	if failure.Kind != "sample_error" || failure.Error != "metrics scrape failed" {
		t.Fatalf("failure record = %+v", failure)
	}
}
