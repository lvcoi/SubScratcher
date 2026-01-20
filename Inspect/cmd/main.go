package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// --- Global State ---
var (
	vulnIPs, vulnPorts, totalDone int64
	startTime                     time.Time
	ipMap                         sync.Map
	printMu                       sync.Mutex // Prevents worker output overlap
	userAgents                    = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
	}
)

func usageInspector() {
	fmt.Printf("\033[1mINSPECTOR PRO\033[0m - Identity & Vulnerability Profiler\n\n")
	fmt.Println("Usage Examples:")
	fmt.Println("  ./knocker -s | ./inspector")
	fmt.Println("  ./inspector -t 8.8.8.8:443")
	fmt.Println("\nFlags:")
	flag.PrintDefaults()
	os.Exit(0)
}

func getRandomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func logFinding(ip, port, details string) {
	f, _ := os.OpenFile("inspector_findings.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	f.WriteString(fmt.Sprintf("[%-19s] %-15s:%-5s | %-20s\n", time.Now().Format("2006-01-02 15:04"), ip, port, details))
}

func main() {
	fileInput := flag.String("f", "", "File of knocker results")
	target := flag.String("t", "", "Direct input (IP:PORT)")
	showHelp := flag.Bool("h", false, "Show help screen")
	flag.Parse()

	if *showHelp {
		usageInspector()
	}

	// Handle Ctrl+C to clean up terminal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Print("\033[r\033[?25h\n\n[!] Interrupted. Terminal Reset.\n")
		os.Exit(0)
	}()

	startTime = time.Now()
	rand.Seed(time.Now().UnixNano())

	var input []string
	if *target != "" {
		input = append(input, *target)
	} else if *fileInput != "" {
		f, err := os.Open(*fileInput)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			input = append(input, s.Text())
		}
		f.Close()
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			s := bufio.NewScanner(os.Stdin)
			for s.Scan() {
				input = append(input, s.Text())
			}
		} else {
			usageInspector()
		}
	}

	// 1. CLEAN THE SCREEN ONCE
	fmt.Print("\033[2J\033[H")

	var wg sync.WaitGroup
	sem := make(chan struct{}, 15)

	// 2. START THE WORKERS - NO REAL-TIME FOOTER

	for _, line := range input {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(l string) {
			defer wg.Done()
			defer func() { <-sem; atomic.AddInt64(&totalDone, 1) }()

			parts := strings.Split(l, ":")
			var ip, port, status, host string
			switch len(parts) {
			case 1:
				ip, port, status, host = parts[0], "80", "Open", "none"
			case 2:
				ip, port, status, host = parts[0], parts[1], "Open", "none"
			case 3:
				ip, port, status, host = parts[0], parts[1], parts[2], "none"
			case 4:
				ip, port, status, host = parts[0], parts[1], parts[2], parts[3]
			default:
				return
			}

			if strings.EqualFold(status, "Open") || strings.EqualFold(status, "Open/Filtered") {
				if port == "80" || port == "443" || port == "8080" || port == "8443" {
					inspectHTTP(ip, port, host)
				} else {
					inspectRaw(ip, port)
				}
			}
		}(line)
	}
	wg.Wait()

	// Finalize: move cursor past footer
	fmt.Print("\033[999;1H\n\n")
	fmt.Printf("\033[32m[+] Scan Complete. Results in inspector_findings.log\033[0m\n")
}

func inspectHTTP(ip, port, host string) {
	// 1. Generate Host Candidates
	hosts := []string{ip} // Start with the "none" / IP-only approach
	if host != "" && host != "none" {
		hosts = append(hosts, host)
	}
	// Add the sister-site to every check to find cross-domain leaks
	if !strings.Contains(host, "xvideos.com") {
		hosts = append(hosts, "xnxx.com", "www.xnxx.com", "xvideos.com")
	}

	for _, h := range hosts {
		proto := "http"
		if port == "443" || port == "8443" {
			proto = "https"
		}
		url := fmt.Sprintf("%s://%s:%s", proto, ip, port)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue // Skip invalid URLs
		}
		req.Host = h // Force the Host Header
		req.Header.Set("User-Agent", getRandomUA())

		client := &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		}

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		// Logic: If IP-based (h == ip) gives 403 but Domain-based gives 200, it's a VULN
		status := resp.StatusCode
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		title := getTitle(string(body))

		printMu.Lock()
		if h != ip && status == 200 {
			// Clear the line before printing a high-priority finding
			fmt.Printf("\r\033[K\033[1;32m[!] VULN FOUND: Host-Header Bypass on %s:%s using Host: %s\033[0m\n", ip, port, h)
			atomic.AddInt64(&vulnIPs, 1)
		}

		fmt.Printf("\r\033[K  port %-5s\tHost: %-15s | Code: %d | Title: %s\n", port, h, status, title)
		printMu.Unlock()

		// If we found a successful hit, we can stop fuzzing this port
		if status == 200 {
			break
		}
	}
}

func inspectRaw(ip, port string) {
	target := net.JoinHostPort(ip, port)
	conn, err := net.DialTimeout("tcp", target, 3*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	// Special Probe Logic for discovered ports
	var probe []byte
	switch port {
	case "111":
		probe = []byte("\x80\x00\x00\x28\x72\xfe\x1d\x13\x00\x00\x00\x00\x00\x00\x00\x02\x00\x01\x86\xa0\x00\x00\x00\x02\x00\x00\x00\x04\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00") // RPC Dump
	case "5666":
		probe = []byte("\x00\x01\x00\x02\x00\x00\x00\x00") // NRPE Packet
	}

	if probe != nil {
		conn.Write(probe)
	}

	buf := make([]byte, 512)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)
	banner := cleanBanner(buf[:n])

	printMu.Lock()
	fmt.Printf("\r\033[K  port %-5s\t\033[35m[RAW SERVICE]\033[0m Banner: %s\n", port, banner)
	if port == "111" && n > 0 {
		fmt.Printf("    \033[33m└── Detected RPCBind. Potential Info Leak.\033[0m\n")
		atomic.AddInt64(&vulnPorts, 1)
	}
	printMu.Unlock()
}

// Helper functions
func getTitle(body string) string {
	re := regexp.MustCompile(`(?i)<title>(.*?)</title>`)
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return "No Title"
}

func cleanBanner(data []byte) string {
	// Clean non-printable characters
	result := make([]byte, 0, len(data))
	for _, b := range data {
		if b >= 32 && b <= 126 || b == 9 || b == 10 || b == 13 {
			result = append(result, b)
		} else {
			result = append(result, '.')
		}
	}
	return strings.TrimSpace(string(result))
}
