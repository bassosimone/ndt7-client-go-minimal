package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
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

	roundTripMaxMessageSize = 1 << 17
	roundTripRuntime        = 3 * time.Second
)

type roundTripRequest struct {
	RTTVar float64       // RTT variance (μs)
	SRTT   float64       // smoothed RTT (μs)
	ST     time.Duration // sender time (μs)
}

func (rrr roundTripRequest) String(elapsed time.Duration) string {
	return fmt.Sprintf(
		`{"AppInfo":{"SRTT":%f,"RTTVar":%f,"ElapsedTime":%d},"Test":"%s"}`,
		rrr.SRTT, rrr.RTTVar, elapsed, "roundtrip")
}

type roundTripReply struct {
	STE time.Duration // sender time echo (μs)
	STD time.Duration // sender time difference (μs)
	RT  time.Duration // receiver time (μs)
}

type roundTripRecvInfo struct {
	msg      roundTripRequest
	recvTime time.Time
}

func roundTripRecv(conn *websocket.Conn) (*roundTripRecvInfo, error) {
	kind, reader, err := conn.NextReader()
	if err != nil {
		return nil, err
	}
	recvTime := time.Now()
	if kind != websocket.TextMessage {
		return nil, errors.New("unexpected message type")
	}
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var info roundTripRecvInfo
	if err := json.Unmarshal(data, &info.msg); err != nil {
		return nil, err
	}
	info.recvTime = recvTime
	return &info, nil
}

func roundTripTest(ctx context.Context, conn *websocket.Conn) error {
	start := time.Now()
	if err := conn.SetReadDeadline(start.Add(roundTripRuntime)); err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(start.Add(roundTripRuntime)); err != nil {
		return err
	}
	conn.SetReadLimit(roundTripMaxMessageSize)
	for ctx.Err() == nil {
		info, err := roundTripRecv(conn)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n\n", info.msg.String(info.recvTime.Sub(start)))
		reply := roundTripReply{
			STE: info.msg.ST,
			STD: info.recvTime.Sub(start)/time.Microsecond - info.msg.ST,
			RT:  time.Since(start) / time.Microsecond,
		}
		if err := conn.WriteJSON(reply); err != nil {
			return err
		}
	}
	return nil
}

func emitAppInfo(start time.Time, total int64, testname string) {
	fmt.Printf(`{"AppInfo":{"NumBytes":%d,"ElapsedTime":%d},"Test":"%s"}`+"\n\n",
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
		kind, reader, err := conn.NextReader()
		if err != nil {
			return err
		}
		if kind == websocket.TextMessage {
			data, err := ioutil.ReadAll(reader)
			if err != nil {
				return err
			}
			total += int64(len(data))
			fmt.Printf("%s\n", string(data))
			continue
		}
		n, err := io.Copy(ioutil.Discard, reader)
		if err != nil {
			return err
		}
		total += int64(n)
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
	flagDownload = flag.String("download", "", "Download URL")
	flagNoVerify = flag.Bool("no-verify", false, "No TLS verify")
	flagUpload   = flag.String("upload", "", "Upload URL")

	flagRoundTrip = flag.String("round-trip", "", "Round trip URL")
)

func dialer(ctx context.Context, URL string) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: *flagNoVerify,
		},
		ReadBufferSize:  maxMessageSize,
		WriteBufferSize: maxMessageSize,
	}
	headers := http.Header{}
	headers.Add("Sec-WebSocket-Protocol", "net.measurementlab.ndt.v7")
	conn, _, err := dialer.DialContext(ctx, URL, headers)
	return conn, err
}

func warnx(err error, testname string) {
	fmt.Printf(`{"Failure":"%s","Test":"%s"}`+"\n\n", err.Error(), testname)
}

func errx(exitcode int, err error, testname string) {
	warnx(err, testname)
	os.Exit(exitcode)
}

const (
	locateDownloadURL = "wss:///ndt/v7/download"
	locateUploadURL   = "wss:///ndt/v7/upload"
)

type locateResponseResult struct {
	URLs map[string]string `json:"urls"`
}

type locateResponse struct {
	Results []locateResponseResult `json:"results"`
}

func locate(ctx context.Context) error {
	// If you don't specify any option then we use locate. Otherwise we assume
	// you're testing locally and we only do what you asked us to do.
	if *flagRoundTrip != "" || *flagDownload != "" || *flagUpload != "" {
		return nil
	}
	resp, err := http.Get("https://locate.measurementlab.net/v2/nearest/ndt/ndt7")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var locate locateResponse
	if err := json.Unmarshal(data, &locate); err != nil {
		return err
	}
	if len(locate.Results) < 1 {
		return errors.New("too few entries")
	}
	// TODO(bassosimone): support flagRoundTrip here when locate v2 is ready
	*flagDownload = locate.Results[0].URLs[locateDownloadURL]
	*flagUpload = locate.Results[0].URLs[locateUploadURL]
	return nil
}

func main() {
	flag.Parse()
	ctx := context.Background()
	var (
		conn *websocket.Conn
		err  error
	)
	if err = locate(ctx); err != nil {
		errx(1, err, "locate")
	}
	if *flagRoundTrip != "" {
		if conn, err = dialer(ctx, *flagRoundTrip); err != nil {
			errx(1, err, "roundtrip")
		}
		if err = roundTripTest(ctx, conn); err != nil {
			warnx(err, "roundtrip")
		}
	}
	if *flagDownload != "" {
		if conn, err = dialer(ctx, *flagDownload); err != nil {
			errx(1, err, "download")
		}
		if err = downloadTest(ctx, conn); err != nil {
			warnx(err, "download")
		}
	}
	if *flagUpload != "" {
		if conn, err = dialer(ctx, *flagUpload); err != nil {
			errx(1, err, "upload")
		}
		if err = uploadTest(ctx, conn); err != nil {
			warnx(err, "upload")
		}
	}
}
