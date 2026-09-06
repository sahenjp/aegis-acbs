package maxsearch

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lasder-ca/aegis-acbs/internal/graph"
	"github.com/lasder-ca/aegis-acbs/internal/search"
)

const RoutingKitCH search.Algorithm = "routingkit-ch"

var ErrRoutingKitCHUnreachableUncertified = errors.New("maxsearch: RoutingKit CH unreachable result is not a 64-bit reachability certificate")

type RoutingKitCHRunner struct {
	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        *bufio.Writer
	stdout       *bufio.Reader
	stderr       bytes.Buffer
	closed       bool
	preprocessNS int64
	fingerprint  string
	graph        *graph.Graph
}

// RoutingKitGraphFingerprint binds a sidecar graph to the exact node-indexed
// weighted directed graph used by Aegis. Coordinates and OSM IDs are excluded
// because CH queries depend on node indices and edge costs only.
func RoutingKitGraphFingerprint(g *graph.Graph) string {
	h := sha256.New()
	var word [8]byte
	write := func(value uint64) {
		binary.LittleEndian.PutUint64(word[:], value)
		_, _ = h.Write(word[:])
	}
	write(uint64(len(g.Nodes)))
	write(uint64(g.EdgeCount))
	for from := range g.Nodes {
		for _, edge := range g.OutEdges(from) {
			write(uint64(from))
			write(uint64(edge.To))
			write(edge.Cost)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NewRoutingKitCHRunner starts a persistent RoutingKit CH process and waits for
// preprocessing to finish. The sidecar graph must be created from g by
// cmd/aegis-routingkit-export. A fingerprint handshake rejects stale or
// mismatched sidecar graphs before any query result can be accepted.
func NewRoutingKitCHRunner(ctx context.Context, binaryPath, graphPath string, g *graph.Graph) (*RoutingKitCHRunner, error) {
	if strings.TrimSpace(binaryPath) == "" || strings.TrimSpace(graphPath) == "" {
		return nil, errors.New("maxsearch: RoutingKit CH binary and graph are required")
	}
	if g == nil || len(g.Nodes) == 0 {
		return nil, errors.New("maxsearch: RoutingKit CH requires a non-empty Aegis graph")
	}
	expectedFingerprint := RoutingKitGraphFingerprint(g)
	cmd := exec.CommandContext(ctx, binaryPath, graphPath)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	runner := &RoutingKitCHRunner{
		cmd:         cmd,
		stdin:       bufio.NewWriterSize(stdinPipe, 64<<10),
		stdout:      bufio.NewReaderSize(stdoutPipe, 1<<20),
		fingerprint: expectedFingerprint,
		graph:       g,
	}
	cmd.Stderr = &runner.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	line, err := runner.stdout.ReadString('\n')
	if err != nil {
		_ = cmd.Wait()
		return nil, runner.protocolError("read READY", err)
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "READY" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, runner.protocolError("invalid READY response", nil)
	}
	preprocessNS, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || preprocessNS < 1 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, runner.protocolError("invalid preprocessing duration", err)
	}
	if fields[2] != expectedFingerprint {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("maxsearch: RoutingKit CH graph fingerprint mismatch: sidecar=%s aegis=%s", fields[2], expectedFingerprint)
	}
	runner.preprocessNS = preprocessNS
	return runner, nil
}

func (r *RoutingKitCHRunner) Name() search.Algorithm { return RoutingKitCH }

func (r *RoutingKitCHRunner) PreprocessDuration() time.Duration {
	return time.Duration(r.preprocessNS)
}

func (r *RoutingKitCHRunner) Fingerprint() string { return r.fingerprint }

func (r *RoutingKitCHRunner) Run(ctx context.Context, g *graph.Graph, source, target int) (search.Result, error) {
	if err := ctx.Err(); err != nil {
		return search.Result{}, err
	}
	if source < 0 || source >= len(g.Nodes) || target < 0 || target >= len(g.Nodes) {
		return search.Result{}, errors.New("maxsearch: source or target is out of range")
	}
	if g != r.graph {
		return search.Result{}, errors.New("maxsearch: RoutingKit CH runner used with a different Aegis graph instance")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return search.Result{}, errors.New("maxsearch: RoutingKit CH runner is closed")
	}

	// A sidecar query is a blocking stream operation. If the caller actually
	// cancels while the query is still outstanding, terminate the sidecar to
	// unblock the read. runPlan also cancels its per-query context immediately
	// after a successful result; queryDone is already closed in that case and
	// must win over the cancellation, otherwise a reusable sidecar can be killed
	// after a perfectly successful query.
	queryDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			select {
			case <-queryDone:
				return
			default:
			}
			if r.cmd.Process != nil {
				_ = r.cmd.Process.Kill()
			}
		case <-queryDone:
		}
	}()
	defer close(queryDone)

	if _, err := fmt.Fprintf(r.stdin, "Q %d %d\n", source, target); err != nil {
		return search.Result{}, r.protocolError("write query", err)
	}
	if err := r.stdin.Flush(); err != nil {
		return search.Result{}, r.protocolError("flush query", err)
	}
	line, err := r.stdout.ReadString('\n')
	if err != nil {
		if ctx.Err() != nil {
			return search.Result{}, ctx.Err()
		}
		return search.Result{}, r.protocolError("read query result", err)
	}
	result, err := parseRoutingKitCHResponse(line, len(g.Nodes))
	if err != nil {
		return search.Result{}, r.protocolError("parse query result", err)
	}
	if !result.Stats.Reachable {
		// RoutingKit uses a 31-bit finite-distance sentinel. A true shortest path
		// above that range can therefore look unreachable. Never promote U to an
		// exact Aegis result; let a native uint64 runner certify reachability.
		return search.Result{}, ErrRoutingKitCHUnreachableUncertified
	}
	if !search.Validate(g, source, target, result) {
		return search.Result{}, errors.New("maxsearch: RoutingKit CH returned an invalid path")
	}
	return result, nil
}

func (r *RoutingKitCHRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	_, writeErr := fmt.Fprintln(r.stdin, "X")
	flushErr := r.stdin.Flush()
	waitErr := r.cmd.Wait()
	if writeErr != nil {
		return writeErr
	}
	if flushErr != nil {
		return flushErr
	}
	if waitErr != nil {
		if r.cmd.ProcessState != nil && !r.cmd.ProcessState.Success() {
			return r.protocolError("wait for RoutingKit CH server", waitErr)
		}
		return waitErr
	}
	return nil
}

func (r *RoutingKitCHRunner) protocolError(stage string, err error) error {
	message := strings.TrimSpace(r.stderr.String())
	switch {
	case err != nil && message != "":
		return fmt.Errorf("maxsearch: RoutingKit CH %s: %s: %w", stage, message, err)
	case err != nil:
		return fmt.Errorf("maxsearch: RoutingKit CH %s: %w", stage, err)
	case message != "":
		return fmt.Errorf("maxsearch: RoutingKit CH %s: %s", stage, message)
	default:
		return fmt.Errorf("maxsearch: RoutingKit CH %s", stage)
	}
}

func parseRoutingKitCHResponse(line string, nodeCount int) (search.Result, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return search.Result{}, errors.New("short response")
	}
	switch fields[0] {
	case "U":
		if len(fields) != 2 {
			return search.Result{}, errors.New("invalid unreachable response")
		}
		duration, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || duration < 1 {
			return search.Result{}, errors.New("invalid duration")
		}
		return search.Result{Stats: search.Stats{Algorithm: RoutingKitCH, DurationNS: duration, Reachable: false}}, nil
	case "R":
		if len(fields) < 5 {
			return search.Result{}, errors.New("invalid reachable response")
		}
		distance, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return search.Result{}, errors.New("invalid distance")
		}
		duration, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || duration < 1 {
			return search.Result{}, errors.New("invalid duration")
		}
		pathLen, err := strconv.Atoi(fields[3])
		if err != nil || pathLen < 1 || len(fields) != 4+pathLen {
			return search.Result{}, errors.New("invalid path length")
		}
		path := make([]int, pathLen)
		for i := range path {
			node, err := strconv.Atoi(fields[4+i])
			if err != nil || node < 0 || node >= nodeCount {
				return search.Result{}, errors.New("invalid path node")
			}
			path[i] = node
		}
		return search.Result{
			Path: path,
			Stats: search.Stats{
				Algorithm: RoutingKitCH,
				DurationNS: duration,
				Distance:   distance,
				Reachable:  true,
				PathNodes:  pathLen,
			},
		}, nil
	case "E":
		return search.Result{}, fmt.Errorf("server error: %s", strings.Join(fields[1:], " "))
	default:
		return search.Result{}, errors.New("unknown response type")
	}
}
