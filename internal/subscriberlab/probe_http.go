package subscriberlab

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type payloadMeasurement struct {
	Completed  bool
	Downloaded string
	Received   int64
	BodyDone   bool
	ElapsedMs  int64
}

type measuredBody struct{ received *atomic.Int64 }

func (w measuredBody) Write(p []byte) (int, error) { w.received.Add(int64(len(p))); return len(p), nil }

func measurePayload(ctx context.Context, upload bool) (payloadMeasurement, error) {
	const payloadSize = 1 << 20
	var received atomic.Int64
	var bodyDone atomic.Bool
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		return payloadMeasurement{}, err
	}
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !upload {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = io.Copy(w, bytes.NewReader(bytes.Repeat([]byte{'d'}, payloadSize)))
				return
			}
			_, err := io.Copy(measuredBody{&received}, http.MaxBytesReader(w, r.Body, payloadSize))
			bodyDone.Store(true)
			if err != nil {
				http.Error(w, "synthetic upload interrupted", 400)
				return
			}
			w.WriteHeader(204)
		})}
	go server.Serve(listener)
	defer server.Close()
	args := []string{"netns", "exec", "md-subscriber-lab", "curl", "--noproxy", "*", "--silent", "--show-error", "--fail", "--max-time", "12", "--output", "/dev/null", "--write-out", "%{size_download}"}
	if upload {
		args = append(args, "--data-binary", "@-")
	}
	args = append(args, "http://10.0.2.2:18080/limit-probe")
	command := exec.CommandContext(ctx, "ip", args...)
	if upload {
		command.Stdin = bytes.NewReader(bytes.Repeat([]byte{'u'}, payloadSize))
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, io.Discard
	started := time.Now()
	sendErr := command.Run()
	return payloadMeasurement{sendErr == nil, strings.TrimSpace(output.String()), received.Load(), bodyDone.Load(), time.Since(started).Milliseconds()}, nil
}

func (m payloadMeasurement) evidence() map[string]any {
	return map[string]any{"completed": m.Completed, "downloadedBytes": m.Downloaded, "serverReceivedBytes": strconv.FormatInt(m.Received, 10), "serverBodyComplete": m.BodyDone, "elapsedMs": strconv.FormatInt(m.ElapsedMs, 10), "attemptedBytes": "1048576"}
}
