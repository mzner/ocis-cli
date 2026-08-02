package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	server := flag.String("url", "https://localhost:9200", "oCIS server URL")
	insecure := flag.Bool("insecure", false, "skip TLS certificate verification")
	timeout := flag.Duration("timeout", 5*time.Minute, "maximum readiness wait")
	flag.Parse()

	if err := waitForServer(*server, *insecure, *timeout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "wait for oCIS: %v\n", err)
		os.Exit(1)
	}
}

func waitForServer(server string, insecure bool, timeout time.Duration) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // explicit disposable integration-server setting
		}
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	target := strings.TrimRight(server, "/") +
		"/.well-known/openid-configuration"
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastErr error
	for {
		request, err := http.NewRequestWithContext(
			ctx, http.MethodGet, target, nil,
		)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			closeErr := response.Body.Close()
			if response.StatusCode == http.StatusOK && closeErr == nil {
				return nil
			}
			lastErr = fmt.Errorf("HTTP %s", response.Status)
			if closeErr != nil {
				lastErr = closeErr
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (last result: %v)", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}
