package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/projectdiscovery/cdncheck"
)

var fileMutex sync.Mutex
var cdnClient = cdncheck.New()

// IPInfo tracks IP frequency and source information
type IPInfo struct {
	Count   int
	Domains []string
	Source  string
}

var registry = make(map[string]*IPInfo)

func newTokenBucket(qps, burst int) <-chan struct{} {
	if qps <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = 1
	}

	tokens := make(chan struct{}, burst)
	for i := 0; i < burst; i++ {
		tokens <- struct{}{}
	}

	interval := time.Second / time.Duration(qps)
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			select {
			case tokens <- struct{}{}:
			default:
			}
		}
	}()

	return tokens
}

func lookupHost(target, resolverAddr string) ([]string, string, error) {
	if ips, ok := lookupLocalHosts(target); ok {
		return ips, "local", nil
	}
	if offlineMode {
		return nil, resolverAddr, fmt.Errorf("offline mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "udp", resolverAddr)
		},
	}

	ips, err := r.LookupHost(ctx, target)
	return ips, resolverAddr, err
}

func scratchWorker(domain string, jobs <-chan string, wg *sync.WaitGroup, delay, jitter int, limiter <-chan struct{}, foundItems *sync.Map, counter *int64, files map[string]*os.File, urlOnly, ipOnly, silent bool, filterCDN bool, wildcardIPs map[string]bool) {
	defer wg.Done()

	for sub := range jobs {
		cleanSub := strings.ToLower(strings.TrimSpace(sub))
		atomic.AddInt64(counter, 1)

		// Visual progress feedback
		if !silent && atomic.LoadInt64(counter)%10 == 0 {
			fmt.Printf("\r\033[K[*] Probing: %s.%s (%d)", cleanSub, domain, atomic.LoadInt64(counter))
		}

		if limiter != nil {
			<-limiter
		}
		sleepWithJitter(delay, jitter)
		target := fmt.Sprintf("%s.%s", cleanSub, domain)
		resolverAddr := getRandomResolver()

		ips, resolverUsed, err := lookupHost(target, resolverAddr)

		if err == nil {
			resolverName := getResolverName(resolverUsed)

			// --- WILDCARD DETECTION ---
			// If all IPs returned match the wildcard pool, this is a fake subdomain
			isWildcardSub := true
			for _, ip := range ips {
				if !wildcardIPs[ip] {
					isWildcardSub = false
					break
				}
			}
			if isWildcardSub {
				continue
			}

			var filteredIPs []string
			var cdnTags []string

			for _, ip := range ips {
				matched, val, errStr, _ := cdnClient.Check(net.ParseIP(ip))
				isCDNProvider := matched && errStr == ""
				isWildcardIP := wildcardIPs[ip]

				// If filtering, skip known CDNs and the Wildcard/Anycast pool
				if filterCDN && (isCDNProvider || isWildcardIP) {
					continue
				}

				filteredIPs = append(filteredIPs, ip)

				// Refined Tagging Logic
				if isCDNProvider {
					cdnTags = append(cdnTags, fmt.Sprintf("[%s CDN]", val))
				} else if isWildcardIP {
					cdnTags = append(cdnTags, "[\033[33mCDN Anycast/Wildcard\033[0m]")
				} else {
					// ONLY tag as TRUE ORIGIN if it passes all filters
					cdnTags = append(cdnTags, "\033[1m\033[32m[TRUE ORIGIN]\033[0m")
				}
			}

			if len(filteredIPs) == 0 {
				continue
			}

			recordKey := fmt.Sprintf("%s-%v", cleanSub, filteredIPs)
			if _, loaded := foundItems.LoadOrStore(recordKey, true); !loaded {
				ipDisplay := strings.Join(filteredIPs, ", ")
				cdnDisplay := strings.Join(cdnTags, ", ")

				if urlOnly {
					fmt.Println(target)
				} else if ipOnly {
					for _, ip := range filteredIPs {
						fmt.Println(ip)
					}
				}

				if !silent {
					fmt.Print("\r\033[K")
					fmt.Printf("\033[32m[+] FOUND:\033[0m %-25s || \033[33mDNS: %-15s\033[0m || \033[36m%s\033[0m || %s\n",
						target, resolverName, ipDisplay, cdnDisplay)
				}

				if len(files) > 0 {
					writeToFiles(files, target, filteredIPs, resolverName)
				}

				for _, ip := range filteredIPs {
					registerIP(ip, target, "Wordlist")
				}
			}
		}
	}
}

// writeToFiles must be outside the scratchWorker function's closing brace
func writeToFiles(files map[string]*os.File, target string, ips []string, resName string) {
	fileMutex.Lock()
	defer fileMutex.Unlock()

	// Since ips is now []string, we can join them into a clean string for the files
	ipStr := strings.Join(ips, ", ")

	if f, ok := files["txt"]; ok {
		fmt.Fprintln(f, target)
	}
	if f, ok := files["csv"]; ok {
		// CSVs often use quotes for fields containing commas
		fmt.Fprintf(f, "%s,\"%s\",%s\n", target, ipStr, resName)
	}
	if f, ok := files["xml"]; ok {
		fmt.Fprintf(f, "  <host><subdomain>%s</subdomain><ips>%s</ips></host>\n", target, ipStr)
	}
	if f, ok := files["grep"]; ok {
		fmt.Fprintf(f, "Host: %s\tIPs: %s\tResolver: %s\tSource: %s\n", target, ipStr, resName, "Wordlist")
	}
}

// filterByFrequency removes IPs that appear too frequently (likely CDN edges)
func filterByFrequency(ips []string, filterCDN bool) []string {
	if !filterCDN {
		return ips
	}

	var filtered []string
	for _, ip := range ips {
		if info, exists := registry[ip]; exists && info.Count <= 3 {
			filtered = append(filtered, ip)
		}
	}
	return filtered
}

// checkCNAMEChaser analyzes common subdomains for CNAME hops and origin IPs
func checkCNAMEChaser(domain string, ipOnly, filterCDN bool) {
	foundIPs := make(map[string]bool)
	subs := []string{"", "www", "dev", "api", "origin", "staging", "internal", "mail", "portal"}

	for _, s := range subs {
		target := domain
		if s != "" {
			target = s + "." + domain
		}

		// Look for CNAME hops
		cname, _ := net.LookupCNAME(target)

		// Resolve IPs
		ips, err := net.LookupIP(target)
		if err != nil {
			continue
		}

		for _, ip := range ips {
			ipStr := ip.String()
			if foundIPs[ipStr] {
				continue
			}
			foundIPs[ipStr] = true

			matched, val, _, _ := cdnClient.Check(ip)
			isCDN := matched

			if ipOnly {
				if filterCDN && isCDN {
					continue
				}
				fmt.Println(ipStr)
			} else {
				// Visual Output
				tag := "[\033[32mPotential Origin\033[0m]"
				if isCDN {
					tag = fmt.Sprintf("[\033[31m%s CDN\033[0m]", val)
				}

				source := "A"
				if cname != "" && cname != target+"." {
					source = fmt.Sprintf("CNAME: %s", strings.TrimSuffix(cname, "."))
				}

				fmt.Printf("%-15s %-25s | %s\n", ipStr, tag, source)
			}
		}
	}
}

// registerIP tracks IP frequency and source information
func registerIP(ip, domain, source string) {
	if _, exists := registry[ip]; !exists {
		registry[ip] = &IPInfo{Source: source}
	}
	registry[ip].Count++
	registry[ip].Domains = append(registry[ip].Domains, domain)
}

// resolveAndRegister resolves a domain and registers its IPs
func resolveAndRegister(target, source string) {
	if ips, ok := lookupLocalHosts(target); ok {
		for _, ip := range ips {
			registerIP(ip, target, source)
		}
		return
	}
	if offlineMode {
		return
	}

	ips, err := net.LookupIP(target)
	if err != nil {
		return
	}
	for _, ip := range ips {
		registerIP(ip.String(), target, source)
	}
}
