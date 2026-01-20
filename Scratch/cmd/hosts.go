package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var localHosts map[string][]string
var offlineMode bool

func loadHostsFile(path string) (map[string][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hosts := make(map[string][]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid hosts entry at line %d", lineNum)
		}
		host := strings.ToLower(fields[0])
		hosts[host] = append(hosts[host], fields[1:]...)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return hosts, nil
}

func lookupLocalHosts(target string) ([]string, bool) {
	if len(localHosts) == 0 {
		return nil, false
	}
	ips, ok := localHosts[strings.ToLower(target)]
	return ips, ok
}
