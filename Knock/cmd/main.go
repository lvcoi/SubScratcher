package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

type PortResult struct {
	Port   int
	Status string
}

var serviceMap = map[int]string{
	7: "Echo", 9: "Discard", 13: "Daytime", 21: "FTP", 22: "SSH", 23: "Telnet", 25: "SMTP",
	37: "Time", 53: "DNS", 79: "Finger", 80: "HTTP", 81: "HTTP-Alt", 88: "Kerberos",
	110: "POP3", 111: "RPCBind", 119: "NNTP", 135: "MSRPC", 139: "NetBIOS", 143: "IMAP",
	161: "SNMP", 389: "LDAP", 443: "HTTPS", 445: "SMB", 465: "SMTPS", 514: "Syslog",
	515: "LPD", 543: "KLogin", 544: "KShell", 548: "AFP", 554: "RTSP", 587: "Submission",
	631: "IPP", 990: "FTPS", 993: "IMAPS", 995: "POP3S", 1433: "MSSQL", 1723: "PPTP",
	3306: "MySQL", 3389: "RDP", 5000: "UPnP", 5432: "Postgres", 5900: "VNC", 6379: "Redis",
	8000: "HTTP-Alt", 8080: "HTTP-Proxy", 8443: "HTTPS-Alt", 10000: "Webmin",
}

func usageKnocker() {
	fmt.Printf("\033[1mKNOCKER\033[0m - High-Speed Port Discovery Tool\n\n")
	fmt.Println("Usage Examples:")
	fmt.Println("  cat ips.txt | ./knocker -s             # Pipe to Inspector")
	fmt.Println("  ./knocker -t 1.1.1.1,8.8.8.8 -v        # Direct scan")
	fmt.Println("  ./knocker -f targets.txt -desc         # File scan with descriptions")
	fmt.Println("\nFlags:")
	flag.PrintDefaults()
	os.Exit(0)
}

func logKnock(data string) {
	f, _ := os.OpenFile("knocker_history.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	f.WriteString(fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), data))
}

func main() {
	targetList := flag.String("t", "", "Comma-separated IPs to scan")
	fileInput := flag.String("f", "", "File containing IPs (one per line)")
	silent := flag.Bool("s", false, "Silent mode for piping (Output: IP:PORT:STATUS:DOMAIN)")
	verbose := flag.Bool("v", false, "Verbose mode (show closed/filtered)")
	udpMode := flag.Bool("udp", false, "UDP mode")
	desc := flag.Bool("desc", false, "Description mode (detailed table)")
	delay := flag.Int("delay", 0, "Delay between ports (ms)")
	domain := flag.String("d", "", "Target domain for host header injection")
	showHelp := flag.Bool("h", false, "Show help screen")
	flag.Parse()

	if *showHelp {
		usageKnocker()
	}

	var targets []string
	if *targetList != "" {
		targets = strings.Split(*targetList, ",")
	} else if *fileInput != "" {
		file, err := os.Open(*fileInput)
		if err != nil {
			fmt.Printf("Error opening file: %v\n", err)
			os.Exit(1)
		}
		s := bufio.NewScanner(file)
		for s.Scan() {
			targets = append(targets, strings.TrimSpace(s.Text()))
		}
		file.Close()
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			s := bufio.NewScanner(os.Stdin)
			for s.Scan() {
				targets = append(targets, strings.TrimSpace(s.Text()))
			}
		} else {
			usageKnocker()
		}
	}

	tcpPorts := []int{7, 9, 13, 21, 22, 23, 25, 26, 37, 53, 79, 80, 81, 88, 106, 110, 111, 113, 119, 135, 139, 143, 144, 179, 199, 389, 427, 443, 444, 445, 465, 513, 514, 515, 543, 544, 548, 554, 587, 631, 646, 873, 990, 993, 995, 1025, 1026, 1027, 1028, 1029, 1110, 1433, 1720, 1723, 1755, 1900, 2000, 2049, 2121, 2717, 3000, 3128, 3306, 3389, 3986, 4899, 5000, 5009, 5051, 5060, 5101, 5190, 5357, 5432, 5631, 5666, 5800, 5900, 6000, 6001, 6646, 7070, 8000, 8008, 8009, 8080, 8081, 8443, 8888, 9100, 9999, 10000, 32768, 49152, 49153, 49154, 49155, 49156, 49157}
	udpPorts := []int{7, 9, 17, 19, 53, 67, 68, 69, 111, 123, 135, 137, 138, 139, 161, 162, 177, 443, 445, 500, 514, 515, 518, 520, 593, 623, 626, 631, 996, 997, 998, 999, 1022, 1023, 1025, 1026, 1027, 1028, 1029, 1030, 1433, 1434, 1645, 1646, 1701, 1718, 1719, 1812, 1813, 1900, 2000, 2048, 2049, 2222, 3130, 3283, 3456, 3703, 4444, 4500, 5000, 5060, 5353, 5355, 5632, 9200, 10000, 17185, 20031, 27015, 27374, 30718, 31337, 32768, 32769, 32771, 32815, 33281, 49152, 49153, 49154, 49156, 49181, 49182, 49185, 49186, 49188, 49189, 49190, 49191, 49192, 49193, 49194, 49200, 49201, 49202}

	targetPorts := tcpPorts
	if *udpMode {
		targetPorts = udpPorts
	}

	ipSem := make(chan struct{}, 5)
	var mainWg sync.WaitGroup
	for _, ip := range targets {
		mainWg.Add(1)
		ipSem <- struct{}{}
		go func(targetIP string) {
			defer mainWg.Done()
			defer func() { <-ipSem }()
			processIP(targetIP, targetPorts, *silent, *verbose, *desc, *udpMode, *delay, *domain)
		}(ip)
	}
	mainWg.Wait()
}

func processIP(ip string, ports []int, silent, verbose, desc, udpMode bool, delay int, domainUsed string) {
	shuffled := make([]int, len(ports))
	copy(shuffled, ports)
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	var results []PortResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, p := range shuffled {
		wg.Add(1)
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		go func(port int) {
			defer wg.Done()
			addr := fmt.Sprintf("%s:%d", ip, port)
			status := "Closed"
			if udpMode {
				status = knockUDP(addr)
			} else {
				status = knockTCP(addr)
			}

			if silent && (status == "Open" || status == "Open/Filtered") {
				d := domainUsed
				if d == "" {
					d = "none"
				}
				fmt.Printf("%s:%d:%s:%s\n", ip, port, status, d)
				logKnock(fmt.Sprintf("%s:%d:%s:%s", ip, port, status, d))
			}

			mu.Lock()
			results = append(results, PortResult{Port: port, Status: status})
			mu.Unlock()
		}(p)
	}
	wg.Wait()

	if !silent {
		sort.Slice(results, func(i, j int) bool { return results[i].Port < results[j].Port })
		if desc {
			printDescriptionTable(ip, results, verbose)
		} else {
			printDashboard(ip, results, verbose)
		}
	}
}

func knockTCP(addr string) string {
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.Dial("tcp", addr)
	if err == nil {
		conn.Close()
		return "Open"
	}
	if strings.Contains(err.Error(), "refused") {
		return "Closed"
	}
	return "Filtered"
}

func knockUDP(addr string) string {
	conn, err := net.DialTimeout("udp", addr, 800*time.Millisecond)
	if err != nil {
		return "Filtered"
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("ping"))
	return "Open/Filtered"
}

// --- UI COMPONENTS ---

func printTableSection(title string, colorCode string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("\n    \033[1m\033[%sm%s:\033[0m\n", colorCode, title)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	for i := 0; i < len(items); i += 4 {
		for j := 0; j < 4; j++ {
			if i+j < len(items) {
				fmt.Fprintf(w, "    %s", items[i+j])
				if j < 3 && i+j+1 < len(items) {
					fmt.Fprintf(w, "\t\033[37m||\033[0m\t")
				}
			}
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}

func printDescriptionTable(ip string, results []PortResult, verbose bool) {
	fmt.Printf("\n\033[1m\033[34m[➔]\033[0m \033[1mREPORT FOR:\033[0m %s\n", ip)
	var open, filtered, closed []string
	for _, r := range results {
		d, ok := serviceMap[r.Port]
		if !ok {
			d = "Unknown"
		}
		switch r.Status {
		case "Open", "Open/Filtered":
			open = append(open, fmt.Sprintf("\033[1m\033[32m%d\t%s\033[0m", r.Port, d))
		case "Filtered":
			filtered = append(filtered, fmt.Sprintf("\033[1m\033[33m%d\t%s\033[0m", r.Port, d))
		case "Closed":
			closed = append(closed, fmt.Sprintf("\033[1m\033[31m%d\t%s\033[0m", r.Port, d))
		}
	}
	printTableSection("OPEN", "32", open)
	if verbose {
		printTableSection("FILTERED", "33", filtered)
		printTableSection("CLOSED", "31", closed)
	}
	fmt.Println("\n" + strings.Repeat("─", 55))
}

func wrapText(input []string, limit int) string {
	var lines []string
	currentLine := "    "
	for _, word := range input {
		if len(currentLine)+len(word)+2 > limit {
			lines = append(lines, strings.TrimSuffix(currentLine, ", "))
			currentLine = "    " + word + ", "
		} else {
			currentLine += word + ", "
		}
	}
	lines = append(lines, strings.TrimSuffix(currentLine, ", "))
	return strings.Join(lines, "\n")
}

func printDashboard(ip string, results []PortResult, verbose bool) {
	fmt.Printf("\n\033[1m\033[34m[➔]\033[0m \033[1mTARGET:\033[0m %s\n", ip)
	var open, filtered, closed []string
	for _, r := range results {
		pStr := fmt.Sprintf("%d", r.Port)
		switch r.Status {
		case "Open", "Open/Filtered":
			open = append(open, pStr)
		case "Filtered":
			filtered = append(filtered, pStr)
		case "Closed":
			closed = append(closed, pStr)
		}
	}
	fmt.Printf("    \033[1m\033[32mOPEN:\033[0m\n")
	if len(open) > 0 {
		fmt.Println(wrapText(open, 50))
	} else {
		fmt.Println("    None Detected")
	}
	if len(filtered) > 0 {
		fmt.Printf("\n    \033[1m\033[33mFILTERED:\033[0m\n")
		fmt.Println(wrapText(filtered, 50))
	}
	if verbose && len(closed) > 0 {
		fmt.Printf("\n    \033[1m\033[31mCLOSED:\033[0m\n")
		fmt.Println(wrapText(closed, 50))
	}
	fmt.Println(strings.Repeat("─", 55))
}
