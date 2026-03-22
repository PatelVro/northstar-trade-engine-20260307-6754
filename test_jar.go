//go:build ignore
// +build ignore

package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// loggingRoundTripper intercepts to prove exactly what is sent to Nginx
type loggingRoundTripper struct {
	Proxied http.RoundTripper
}

func (lrt *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Printf("\n--- REQUESTING %s %s ---\n", req.Method, req.URL.String())
	fmt.Printf("Outgoing Cookie Header: %q\n", req.Header.Get("Cookie"))
	return lrt.Proxied.RoundTrip(req)
}

func main() {
	jar, _ := cookiejar.New(nil)
	baseTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{
		Transport: &loggingRoundTripper{Proxied: baseTransport},
		Jar:       jar,
		Timeout:   10 * time.Second,
	}

	baseURL := "https://127.0.0.1:5002/v1/api"

	// Helper to reduce boilerplate
	doReq := func(method, endpoint string) ([]byte, int) {
		url := baseURL + endpoint
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			log.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")

		log.Printf("----------------------------------")
		log.Printf("Pre-request JAR cookies for %s: %v\n", req.URL, jar.Cookies(req.URL))
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("EOF Body read failed: %v", err)
		}

		fmt.Printf("Status: %d\n", resp.StatusCode)

		out := string(b)
		if len(out) > 400 {
			fmt.Printf("Body (truncated): %s...\n", out[:400])
		} else {
			fmt.Printf("Body: %s\n", out)
		}
		return b, resp.StatusCode
	}

	// 1. Check Auth Status
	bAuth, statusAuth := doReq("GET", "/iserver/auth/status")
	var authResp struct {
		Authenticated bool `json:"authenticated"`
	}
	json.Unmarshal(bAuth, &authResp)

	if !authResp.Authenticated || statusAuth != 200 {
		log.Printf(" auth/status is NOT authenticated: %s. Need reauth flow.", string(bAuth))
		return
	}

	fmt.Println(" auth/status is authenticated!")
	time.Sleep(2 * time.Second)

	// 2. Init /iserver/accounts
	doReq("GET", "/iserver/accounts")
	time.Sleep(2 * time.Second)

	// 3. Get Account ID from portfolio
	bPort, statusPort := doReq("GET", "/portfolio/accounts")
	if statusPort != 200 {
		log.Fatal(" portfolio/accounts failed")
	}

	var accounts []struct {
		ID string `json:"id"`
	}
	var accountId string
	if err := json.Unmarshal(bPort, &accounts); err == nil && len(accounts) > 0 {
		accountId = accounts[0].ID
	} else {
		log.Fatal("Failed to parse account ID from portfolio/accounts payload")
	}
	fmt.Printf(" Discovered Account ID: %s\n", accountId)
	time.Sleep(2 * time.Second)

	runTests := func(accountId string, iteration int) bool {
		fmt.Printf("\n========== ITERATION %d ==========\n", iteration)

		// 4a. Candidate 1: iserver/account/search
		doReq("GET", fmt.Sprintf("/iserver/account/search/%s", accountId))
		time.Sleep(1 * time.Second)

		// 4b. Candidate 2: dynaccount
		doReq("POST", fmt.Sprintf("/iserver/dynaccount?acctId=%s", accountId))
		time.Sleep(1 * time.Second)

		// 5. Test Summary
		_, statusSum := doReq("GET", fmt.Sprintf("/portfolio/%s/summary", accountId))
		if statusSum == 200 {
			fmt.Printf(" SUCCESS: Summary returned 200 OK on iteration %d!\n", iteration)
			return true
		}
		fmt.Printf(" FAILED: Summary returned %d on iteration %d.\n", statusSum, iteration)
		return false
	}

	successCount := 0
	for i := 1; i <= 5; i++ {
		if runTests(accountId, i) {
			successCount++
		}
		time.Sleep(3 * time.Second)
	}

	fmt.Printf("\nFinal Score: %d/5 Summary Checks Passed\n", successCount)
}
