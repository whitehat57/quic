package main

import (
    "bytes"
    "compress/gzip"
    "context"
    "crypto/tls"
    "flag"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "net/url"
    "sync"
    "time"

    "golang.org/x/net/http2"
    "golang.org/x/net/http3"
)

// Warna untuk log
const (
    colorReset  = "\033[0m"
    colorGreen  = "\033[32m"
    colorRed    = "\033[31m"
    colorYellow = "\033[33m"
)

func main() {
    // Argument parsing
    urlStr := flag.String("u", "", "Target URL for stress testing")
    method := flag.String("m", "GET", "HTTP method (GET or POST)")
    rateLimit := flag.Int("r", 10, "Rate limit (requests per second)")
    concurrent := flag.Int("c", 5, "Number of concurrent requests")
    duration := flag.Duration("d", 30*time.Second, "Duration of the stress test")
    flag.Parse()

    if *urlStr == "" {
        log.Fatal("URL is required. Use -u to specify the target URL.")
    }

    // Parse the URL
    parsedURL, err := url.Parse(*urlStr)
    if err != nil {
        log.Fatalf("Invalid URL: %v", err)
    }

    // Create a context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), *duration)
    defer cancel()

    // Rate limiter
    rateLimiter := time.Tick(time.Second / time.Duration(*rateLimit))

    // WaitGroup for concurrent requests
    var wg sync.WaitGroup

    // Channel to collect status codes
    statusChan := make(chan int, 100)

    // Goroutine to log status codes every second
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                logStatusCounts(statusChan)
            }
        }
    }()

    // Start stress testing
    for i := 0; i < *concurrent; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case <-ctx.Done():
                    return
                case <-rateLimiter:
                    statusCode, err := sendRequest(parsedURL, *method)
                    if err != nil {
                        log.Printf("%sError sending request: %v%s", colorRed, err, colorReset)
                    } else {
                        statusChan <- statusCode
                    }
                }
            }
        }()
    }

    // Wait for all goroutines to finish
    wg.Wait()
    close(statusChan)
    fmt.Println("\nStress test completed.")
}

func sendRequest(targetURL *url.URL, method string) (int, error) {
    // Prepare the request body (gzip compressed)
    var requestBody bytes.Buffer
    gzipWriter := gzip.NewWriter(&requestBody)
    _, err := gzipWriter.Write([]byte("This is a stress test payload"))
    if err != nil {
        return 0, fmt.Errorf("failed to write gzip payload: %w", err)
    }
    gzipWriter.Close()

    // Create the HTTP request
    req, err := http.NewRequest(method, targetURL.String(), &requestBody)
    if err != nil {
        return 0, fmt.Errorf("failed to create request: %w", err)
    }

    // Set headers
    req.Header.Set("Content-Encoding", "gzip")
    req.Header.Set("Content-Type", "application/json")

    // Determine the protocol based on the URL scheme
    var client *http.Client
    switch targetURL.Scheme {
    case "https":
        client = &http.Client{
            Transport: &http3.RoundTripper{},
        }
    case "h2":
        client = &http.Client{
            Transport: &http2.Transport{
                TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
            },
        }
    default:
        client = &http.Client{}
    }

    // Send the request
    resp, err := client.Do(req)
    if err != nil {
        return 0, fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    // Read and discard the response body
    _, err = ioutil.ReadAll(resp.Body)
    if err != nil {
        return 0, fmt.Errorf("failed to read response body: %w", err)
    }

    return resp.StatusCode, nil
}

func logStatusCounts(statusChan chan int) {
    statusCounts := make(map[int]int)
    total := 0

    // Collect status codes from the channel
    for {
        select {
        case statusCode, ok := <-statusChan:
            if !ok {
                break
            }
            statusCounts[statusCode]++
            total++
        default:
            break
        }
        if len(statusChan) == 0 {
            break
        }
    }

    // Print status counts
    fmt.Printf("\n%s--- Status Code Summary ---%s\n", colorYellow, colorReset)
    for code, count := range statusCounts {
        color := colorGreen
        if code >= 400 && code < 500 {
            color = colorRed
        } else if code >= 500 {
            color = colorYellow
        }
        fmt.Printf("%s%d: %d%s\n", color, code, count, colorReset)
    }
    fmt.Printf("%sTotal Requests: %d%s\n", colorYellow, total, colorReset)
}
