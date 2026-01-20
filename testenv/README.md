# Test Environment

This directory provides a local test harness for Inspect, Knock, and Scratch.

## Start the mock services

```sh
go run ./testenv/cmd/mockenv
```

Defaults:
- HTTP: 127.0.0.1:8080
- HTTPS: 127.0.0.1:8443
- RAW TCP: 127.0.0.1:5666
- Allowed Host header: allowed.test

To change ports or allowed host headers:

```sh
go run ./testenv/cmd/mockenv -http 18080 -https 18443 -raw 15666 -allow allowed.test,alt.test
```

## Knock (scan localhost)

```sh
go run ./Knock/cmd/main.go -t 127.0.0.1 -desc
```

## Inspect (pipe from Knock)

```sh
go run ./Knock/cmd/main.go -t 127.0.0.1 -s -d allowed.test | go run ./Inspect/cmd/main.go
```

You can also feed Inspect directly:

```sh
echo "127.0.0.1:8080:Open:allowed.test" | go run ./Inspect/cmd/main.go
```

## Scratch (offline with local hosts map)

```sh
go run ./Scratch/cmd/main.go -d local.test -w ./testenv/wordlist.txt -hosts ./testenv/hosts.txt -offline -url
```

`-hosts` format is one host per line:

```
host ip1 [ip2...]
```

Use `-offline` to skip external DNS/CT/SPF lookups during testing.
