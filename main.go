package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

const (
	minMessageSize       = 1 << 10
	maxScaledMessageSize = 1 << 20
	maxMessageSize       = 1 << 24
	maxRuntime           = 10 * time.Second
	measureInterval      = 250 * time.Millisecond
	fractionForScaling   = 16
)

func emitAppInfo(start time.Time, total int64, testname string) {
	fmt.Printf(`{"AppInfo":{"NumBytes":%d,"ElapsedTime":%d},"Test":"%s"}`+"\n",
		total, time.Since(start)/time.Microsecond, testname)
}

func downloadTest(ctx context.Context, conn *websocket.Conn) error {
	var total int64
	start := time.Now()
	if err := conn.SetReadDeadline(start.Add(maxRuntime)); err != nil {
		return err
	}
	conn.SetReadLimit(maxMessageSize)
	ticker := time.NewTicker(measureInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if kind == websocket.TextMessage {
			fmt.Printf("%s\n", string(data))
		}
		total += int64(len(data))
		select {
		case <-ticker.C:
			emitAppInfo(start, total, "download")
		default:
			// NOTHING
		}
	}
	return nil
}

func newMessage(n int) (*websocket.PreparedMessage, error) {
	return websocket.NewPreparedMessage(websocket.BinaryMessage, make([]byte, n))
}

func uploadTest(ctx context.Context, conn *websocket.Conn) error {
	var total int64
	start := time.Now()
	if err := conn.SetWriteDeadline(time.Now().Add(maxRuntime)); err != nil {
		return err
	}
	size := minMessageSize
	message, err := newMessage(size)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(measureInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := conn.WritePreparedMessage(message); err != nil {
			return err
		}
		total += int64(size)
		select {
		case <-ticker.C:
			emitAppInfo(start, total, "upload")
		default:
			// NOTHING
		}
		if int64(size) >= maxScaledMessageSize || int64(size) >= (total/fractionForScaling) {
			continue
		}
		size <<= 1
		if message, err = newMessage(size); err != nil {
			return err
		}
	}
	return nil
}

var (
	flagHostname = flag.String("hostname", "127.0.0.1", "Host to connect to")
	flagNoVerify = flag.Bool("no-verify", false, "No TLS verify")
	flagPort     = flag.String("port", "443", "Port to connect to")
	flagScheme   = flag.String("scheme", "wss", "Scheme to use")
)

func dialer(ctx context.Context, testname string) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: *flagNoVerify,
		},
		ReadBufferSize:  maxMessageSize,
		WriteBufferSize: maxMessageSize,
	}
	URL := url.URL{
		Scheme: *flagScheme,
		Host:   *flagHostname + ":" + *flagPort,
	}
	URL.Path = "/ndt/v7/" + testname
	headers := http.Header{}
	headers.Add("Sec-WebSocket-Protocol", "net.measurementlab.ndt.v7")
	conn, _, err := dialer.DialContext(ctx, URL.String(), headers)
	return conn, err
}

func warnx(err error, testname string) {
	fmt.Printf(`{"Failure":"%s","Test":"%s"}`+"\n", err.Error(), testname)
}

func errx(exitcode int, err error, testname string) {
	warnx(err, testname)
	os.Exit(exitcode)
}

func main() {
	flag.Parse()
	ctx := context.Background()
	var (
		conn *websocket.Conn
		err  error
	)
	if conn, err = dialer(ctx, "download"); err != nil {
		errx(1, err, "donwload")
	}
	if err = downloadTest(ctx, conn); err != nil {
		warnx(err, "download")
	}
	if conn, err = dialer(ctx, "upload"); err != nil {
		errx(1, err, "upload")
	}
	if err = uploadTest(ctx, conn); err != nil {
		warnx(err, "upload")
	}
}
