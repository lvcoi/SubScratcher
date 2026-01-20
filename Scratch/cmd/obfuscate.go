package main

import (
	"math/rand"
	"time"
)

// Resolver Mapping
var resolverNames = map[string]string{
	"8.8.8.8:53":         "Google",
	"1.1.1.1:53":         "Cloudflare",
	"9.9.9.9:53":         "Quad9",
	"208.67.222.222:53":  "OpenDNS",
	"8.8.4.4:53":         "Google-2",
	"1.0.0.1:53":         "Cloudflare-2",
	"149.112.112.112:53": "Quad9-2",
}

func getRandomResolver() string {
	keys := make([]string, 0, len(resolverNames))
	for k := range resolverNames {
		keys = append(keys, k)
	}
	return keys[rand.Intn(len(keys))]
}

func getResolverName(ip string) string {
	if ip == "local" {
		return "LocalHosts"
	}
	if name, ok := resolverNames[ip]; ok {
		return name
	}
	return ip
}

func sleepWithJitter(base, jitter int) {
	if base > 0 {
		ms := base + rand.Intn(jitter+1)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}
