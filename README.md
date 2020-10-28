# Minimal ndt7 client written in Go

This repository contains a minimal ndt7 client written in Go. It lacks many
functionality implemented by [better clients](
https://github.com/m-lab/ndt7-client-go). It's used as a benchmark to make
sure the production server still works with minimal clients.

You need Go >= 1.13 and Python >= 3.7. To run a ndt7 test, type:

```bash
go run main.go | ./ndt7-client-aux
```

The `./ndt7-clien-aux` script just pretty prints the JSON output emitted by
the `main.go` ndt7 implementation. For more fine grained control, try:

```bash
go run main.go -help
```

To run a TLS test towards a test server deployed at `${address}` try:

```bash
sudo ./enable-bbr.bash
go run main.go -no-verify -dowload wss://${hostname}/ndt/v7/download \
                          -upload wss://${hostname}/ndt/v7/upload
```
