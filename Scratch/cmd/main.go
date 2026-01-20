package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// getMapKeys converts map keys to slice for display
func getMapKeys(m map[string]bool) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// fetchWordlist downloads wordlist from URL if needed, otherwise reads from file
func fetchWordlist(path string) ([]string, error) {
	// Check if it's a URL
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		fmt.Printf("[*] Fetching wordlist from: %s\n", path)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(path)
		if err != nil {
			return nil, fmt.Errorf("failed to download wordlist: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("wordlist download failed with status: %d", resp.StatusCode)
		}

		var lines []string
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				lines = append(lines, line)
			}
		}

		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("error reading wordlist: %v", err)
		}

		fmt.Printf("[+] Downloaded %d words from online wordlist\n", len(lines))
		return lines, nil
	}

	// Otherwise, read from local file
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open wordlist file: %v", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading wordlist: %v", err)
	}

	return lines, nil
}

func main() {
	// 1. FLAGS
	domain := flag.String("d", "", "Target domain")
	wordlist := flag.String("w", "subs.txt", "Path to wordlist or URL")
	csvOut := flag.Bool("csv", false, "Output in CSV")
	txtOut := flag.Bool("txt", false, "Output in TXT")
	xmlOut := flag.Bool("xml", false, "Output in XML")
	grepOut := flag.Bool("grep", false, "Output in Grepable format")
	urlOnly := flag.Bool("url", false, "Output raw URLs only")
	ipOnly := flag.Bool("ip", false, "Output raw IPs only")
	threads := flag.Int("t", 10, "Number of workers")
	qps := flag.Int("qps", 5, "Global DNS query rate limit (queries/sec, 0 = unlimited)")
	burst := flag.Int("burst", 2, "Rate limiter burst size")
	delay := flag.Int("delay", 0, "Base delay (ms)")
	jitter := flag.Int("jitter", 0, "Jitter (ms)")
	filterCDN := flag.Bool("filter", false, "Tag or hide CDN/Cloud IPs")
	hostsFile := flag.String("hosts", "", "Local hosts map for offline testing (format: host ip1 [ip2...])")
	offline := flag.Bool("offline", false, "Disable external DNS/CT/SPF lookups (useful with -hosts)")
	flag.Parse()

	if *domain == "" {
		fmt.Println("[!] Usage: ./scratch -d <domain> [-url] [-ip]")
		os.Exit(1)
	}

	// 2. INITIALIZATION
	foundItems := sync.Map{}
	var processedCount int64
	silent := *urlOnly || *ipOnly
	offlineMode = *offline

	if *hostsFile != "" {
		hosts, err := loadHostsFile(*hostsFile)
		if err != nil {
			fmt.Printf("[!] Hosts file error: %v\n", err)
			os.Exit(1)
		}
		localHosts = hosts
		if !silent {
			fmt.Printf("[*] Loaded %d host entries from %s\n", len(localHosts), *hostsFile)
		}
	}

	// 3. WILDCARD DETECTION (Phase 1)
	wildcardIPs := make(map[string]bool)
	if !offlineMode {
		if !silent {
			fmt.Println("[*] Detecting wildcard responses...")
		}

		// Query a random string that definitely shouldn't exist
		wildcardDomain := fmt.Sprintf("check-wildcard-random-999.%s", *domain)
		res, _ := net.LookupHost(wildcardDomain)
		for _, ip := range res {
			wildcardIPs[ip] = true
		}

		if len(wildcardIPs) > 0 && !silent {
			fmt.Printf("[+] Detected %d wildcard IP(s): %v\n", len(wildcardIPs), getMapKeys(wildcardIPs))
		}
	} else if !silent {
		fmt.Println("[*] Offline mode enabled. Skipping wildcard detection.")
	}

	// 4. START WORKERS
	files := make(map[string]*os.File)
	if *csvOut {
		files["csv"] = createOutput(*domain, "csv")
	}
	if *txtOut {
		files["txt"] = createOutput(*domain, "txt")
	}
	if *xmlOut {
		files["xml"] = createOutput(*domain, "xml")
	}
	if *grepOut {
		files["grep"] = createOutput(*domain, "grep")
	}

	defer func() {
		for ext, f := range files {
			if ext == "xml" {
				fmt.Fprintln(f, "</subdomains>")
			}
			f.Close()
		}
	}()

	// 5. START WORKERS
	jobs := make(chan string)
	var wg sync.WaitGroup

	limiter := newTokenBucket(*qps, *burst)

	for i := 0; i < *threads; i++ {
		wg.Add(1)
		go scratchWorker(*domain, jobs, &wg, *delay, *jitter, limiter, &foundItems, &processedCount, files, *urlOnly, *ipOnly, silent, *filterCDN, wildcardIPs)
	}

	// 4. INGESTION (The critical part)
	words, err := fetchWordlist(*wordlist)
	if err != nil {
		fmt.Printf("[!] Wordlist Error: %v\n", err)
		close(jobs) // Close it so workers don't hang if file fails
		return
	}

	for _, line := range words {
		jobs <- line
	}

	// 5. THE SIGNAL & WAIT
	close(jobs) // Tell workers no more data is coming
	wg.Wait()   // Wait for them to finish current tasks

	if !silent {
		fmt.Print("\r\033[K")
		fmt.Println("[*] Scan Complete. All workers have exited.")
	}

	// 6. SPF/TXT RECORD ANALYSIS FOR ORIGIN IP LEAKS
	if !offlineMode {
		if !silent {
			fmt.Println("[*] Checking SPF/TXT records for origin IP leaks...")
		}
		checkSPFLeaks(*domain, *ipOnly, silent)
	} else if !silent {
		fmt.Println("[*] Offline mode enabled. Skipping SPF/TXT checks.")
	}

	// 7. CNAME CHASER LOGIC
	if !silent {
		fmt.Printf("\n\033[1m\033[34m[*] CNAME CHASER ANALYSIS:\033[0m %s\n", *domain)
		fmt.Println(strings.Repeat("━", 40))
	}
	commonSubs := []string{"", "www", "dev", "api", "origin", "mail", "internal", "staging"}
	for _, s := range commonSubs {
		target := *domain
		if s != "" {
			target = s + "." + *domain
		}
		resolveAndRegister(target, "DNS/CNAME")
	}

	// 8. CERTIFICATE TRANSPARENCY SUBDOMAIN DISCOVERY
	if !silent {
		fmt.Printf("\n\033[1m\033[34m[*] CERTIFICATE TRANSPARENCY DISCOVERY:\033[0m %s\n", *domain)
		fmt.Println(strings.Repeat("━", 40))
	}
	if !offlineMode {
		ctSubs := fetchCTSubdomains(*domain)
		if len(ctSubs) > 0 {
			if !silent {
				fmt.Printf("[+] Found %d subdomains from CT logs\n", len(ctSubs))
			}
			for _, s := range ctSubs {
				resolveAndRegister(s, "CT Log")
			}
		} else if !silent {
			fmt.Println("[-] No CT subdomains found")
		}
	} else if !silent {
		fmt.Println("[*] Offline mode enabled. Skipping CT discovery.")
	}

	// 9. INFRASTRUCTURE FINGERPRINTING (Subnet-Based Anomaly Detection)
	if !silent {
		fmt.Printf("\n\033[1m\033[34m[!] INFRASTRUCTURE ANALYSIS FOR: %s\033[0m\n", *domain)
		fmt.Println(strings.Repeat("━", 60))
	}

	// Build IP registry from global registry
	ipRegistry := make(map[string][]string)
	for ip, info := range registry {
		ipRegistry[ip] = info.Domains
	}

	// Group IPs by /24 subnet
	subnets := make(map[string]*SubnetGroup)
	for ip, domains := range ipRegistry {
		// Calculate the /24 subnet (e.g., 185.88.181.0)
		octets := strings.Split(ip, ".")
		if len(octets) != 4 {
			continue
		}
		cidr := fmt.Sprintf("%s.%s.%s.0/24", octets[0], octets[1], octets[2])

		if _, exists := subnets[cidr]; !exists {
			subnets[cidr] = &SubnetGroup{IPs: make(map[string]bool)}
		}
		subnets[cidr].IPs[ip] = true
		subnets[cidr].Hosts += len(domains)
	}

	// Output subnet analysis
	for cidr, group := range subnets {
		// Check the first IP in the subnet for CDN status
		firstIP := ""
		for k := range group.IPs {
			firstIP = k
			break
		}
		matched, provider, _, _ := cdnClient.Check(net.ParseIP(firstIP))
		isCDN := matched

		if *filterCDN && isCDN {
			continue
		}

		// Logic: If a /24 subnet has hundreds of host associations, it's a Cluster.
		// If it only has 1 or 2, it's a specific server (The "Better" Target).
		status := "\033[32m[UNIQUE ORIGIN]\033[0m"
		if isCDN {
			status = fmt.Sprintf("\033[31m[CDN: %s]\033[0m", provider)
		} else if group.Hosts > 10 {
			status = "\033[33m[SHARED INFRA]\033[0m"
		}

		if *ipOnly {
			for ip := range group.IPs {
				fmt.Println(ip)
			}
		} else if *urlOnly {
			for ip := range group.IPs {
				for _, domain := range ipRegistry[ip] {
					fmt.Println(domain)
				}
			}
		} else {
			fmt.Printf("%-18s %s\n", cidr, status)
			for ip := range group.IPs {
				fmt.Printf("  └── %-15s (%d subdomains)\n", ip, len(ipRegistry[ip]))
			}
			fmt.Println()
		}
	}
}

// IPDetail tracks IP and associated domains
type IPDetail struct {
	IP      string
	Domains []string
}

// SubnetGroup groups IPs by /24 subnet
type SubnetGroup struct {
	IPs   map[string]bool
	Hosts int
}

// createOutput is a HELPER function, it should be simple and clean.
func createOutput(domain, ext string) *os.File {
	filename := fmt.Sprintf("%s_recon.%s", domain, ext)
	f, err := os.Create(filename)
	if err != nil {
		fmt.Printf("[!] Could not create %s: %v\n", filename, err)
		return nil
	}

	switch ext {
	case "csv":
		fmt.Fprintln(f, "subdomain,ips,resolver")
	case "xml":
		fmt.Fprintln(f, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<subdomains>")
	case "grep":
		fmt.Fprintf(f, "# Subscratcher Grepable Report for %s\n", domain)
	}
	return f
}

// checkSPFLeaks analyzes SPF/TXT records for potential origin IP leaks
func checkSPFLeaks(domain string, ipOnly, silent bool) {
	txts, err := net.LookupTXT(domain)
	if err != nil {
		if !silent {
			fmt.Printf("[!] No TXT records found for %s\n", domain)
		}
		return
	}

	for _, txt := range txts {
		if strings.Contains(txt, "ip4:") {
			parts := strings.Split(txt, " ")
			for _, p := range parts {
				if strings.HasPrefix(p, "ip4:") {
					ip := strings.TrimPrefix(p, "ip4:")
					registerIP(ip, domain, "SPF Leak")

					if ipOnly {
						fmt.Println(ip)
					} else if !silent {
						fmt.Printf("%s [\033[33mSPF Leak\033[0m]\n", ip)
					}
				}
			}
		}
	}
}

// fetchCTSubdomains queries crt.sh for certificate transparency subdomains
func fetchCTSubdomains(domain string) []string {
	var subs []string
	url := fmt.Sprintf("https://crt.sh/?q=%%.%s&output=json", domain)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return subs
	}
	defer resp.Body.Close()

	var results []struct {
		NameValue string `json:"name_value"`
	}
	json.NewDecoder(resp.Body).Decode(&results)

	for _, res := range results {
		s := strings.Replace(res.NameValue, "*.", "", -1)
		subs = append(subs, s)
	}
	return subs
}
