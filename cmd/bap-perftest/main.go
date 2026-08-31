package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"cc-filter/internal/policybundle"
)

func main() {
	baseURL := flag.String("url", "https://127.0.0.1:8443", "BAP Service URL")
	caPath := flag.String("ca", "", "CA bundle")
	apiKeyEnv := flag.String("api-key-env", "BAP_EDGE_API_KEY", "API key environment variable")
	requests := flag.Int("requests", 200, "total requests")
	concurrency := flag.Int("concurrency", 10, "concurrent workers")
	flag.Parse()
	if *requests <= 0 || *concurrency <= 0 {
		fatal("requests and concurrency must be positive")
	}
	apiKey := os.Getenv(*apiKeyEnv)
	if apiKey == "" {
		fatal(*apiKeyEnv + " is empty")
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if *caPath != "" {
		pem, err := os.ReadFile(*caPath)
		if err != nil || !roots.AppendCertsFromPEM(pem) {
			fatal("could not load CA bundle")
		}
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		MaxIdleConns: 2 * *concurrency, MaxIdleConnsPerHost: 2 * *concurrency,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
	}}
	jobs := make(chan int)
	durations := make([]time.Duration, *requests)
	var failures atomic.Int64
	var workers sync.WaitGroup
	started := time.Now()
	for worker := 0; worker < *concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				begin := time.Now()
				if err := synchronize(context.Background(), client, *baseURL, apiKey, index); err != nil {
					failures.Add(1)
				}
				durations[index] = time.Since(begin)
			}
		}()
	}
	for index := 0; index < *requests; index++ {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	elapsed := time.Since(started)
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	result := map[string]any{
		"requests": *requests, "concurrency": *concurrency, "failures": failures.Load(),
		"elapsed_seconds": elapsed.Seconds(), "requests_per_second": float64(*requests) / elapsed.Seconds(),
		"p50_ms": percentile(durations, 50).Seconds() * 1000,
		"p95_ms": percentile(durations, 95).Seconds() * 1000,
		"p99_ms": percentile(durations, 99).Seconds() * 1000,
	}
	output, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(output))
	if failures.Load() != 0 {
		os.Exit(1)
	}
}

func synchronize(ctx context.Context, client *http.Client, baseURL, apiKey string, index int) error {
	request := policybundle.SyncRequest{EdgeInstanceID: fmt.Sprintf("performance-edge-%d", index), EdgeVersion: "1", Nonce: fmt.Sprintf("performance-%d", index)}
	body, _ := json.Marshal(request)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/bap/v1/edge/sync", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := client.Do(httpRequest)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var responseValue policybundle.SyncResponse
	if err := json.Unmarshal(data, &responseValue); err != nil || len(responseValue.Envelope.Payload) == 0 {
		return fmt.Errorf("unexpected policy sync response")
	}
	return nil
}

func percentile(values []time.Duration, percent int) time.Duration {
	index := (len(values)*percent + 99) / 100
	if index < 1 {
		index = 1
	}
	return values[index-1]
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(2)
}
