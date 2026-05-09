package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	url := "http://127.0.0.1:17180/healthz"
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		url = strings.TrimSpace(os.Args[1])
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "unexpected status: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !strings.Contains(strings.ToLower(string(body)), "service") {
		fmt.Fprintln(os.Stderr, "health response missing service field")
		os.Exit(1)
	}
}
