# Minimal ndt7 client written in Go

This repository contains a minimal ndt7 client written in Go. It lacks many
functionality implemented by [better clients](
https://github.com/m-lab/ndt7-client-go). It's used as a benchmark to make
sure the production server still works with minimal clients.

To build, make sure you have Go >= 1.11 installed and then run

```bash
./build.bash
```

The high level interface to perform a test is

```bash
./ndt7-client
```

To run a TLS test towards a test server deployed at `${address}` try:

```bash
sudo ./enable-bbr.bash
./ndt7-client-bin -no-verify -hostname ${address} | ./ndt7-client-aux
```

For more fine grained control, try

```bash
./ndt7-client-bin -h
```

And follow the instructions printed on the screen.
