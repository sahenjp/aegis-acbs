package maxsearch

import (
	"bufio"
	"bytes"
	"context"
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

const RoutingKitCCH search.Algorithm = "routingkit-cch"

var ErrRoutingKitCCHUnreachableUncertified = errors.New("maxsearch: RoutingKit CCH unreachable result is not a 64-bit reachability certificate")

type RoutingKitCCHRunner struct {
	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        *bufio.Writer
	stdout       *bufio.Reader
	stderr       bytes.Buffer
	closed       bool
	orderNS      int64
	topologyNS   int64
	customizeNS  int64
	fingerprint  string
	graph        *graph.Graph
}

func NewRoutingKitCCHRunner(ctx context.Context, binaryPath, graphPath string, g *graph.Graph) (*RoutingKitCCHRunner, error) {
	if strings.TrimSpace(binaryPath) == "" || strings.TrimSpace(graphPath) == "" {
		return nil, errors.New("maxsearch: RoutingKit CCH binary and graph are required")
	}
	if g == nil || len(g.Nodes) == 0 {
		return nil, errors.New("maxsearch: RoutingKit CCH requires a non-empty Aegis graph")
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
	runner := &RoutingKitCCHRunner{
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
	if len(fields) != 5 || fields[0] != "READY" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, runner.protocolError("invalid READY response", nil)
	}
	orderNS, err := parsePositiveInt64(fields[1])
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, runner.protocolError("invalid order duration", err)
	}
	topologyNS, err := parsePositiveInt64(fields[2])
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, runner.protocolError("invalid topology duration", err)
	}
	customizeNS, err := parsePositiveInt64(fields[3])
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, runner.protocolError("invalid customization duration", err)
	}
	if fields[4] != expectedFingerprint {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("maxsearch: RoutingKit CCH graph fingerprint mismatch: sidecar=%s aegis=%s", fields[4], expectedFingerprint)
	}
	runner.orderNS = orderNS
	runner.topologyNS = topologyNS
	runner.customizeNS = customizeNS
	return runner, nil
}

func (r *RoutingKitCCHRunner) Name() search.Algorithm { return RoutingKitCCH }
func (r *RoutingKitCCHRunner) OrderDuration() time.Duration { return time.Duration(r.orderNS) }
func (r *RoutingKitCCHRunner) TopologyDuration() time.Duration { return time.Duration(r.topologyNS) }
func (r *RoutingKitCCHRunner) CustomizeDuration() time.Duration { return time.Duration(r.customizeNS) }
func (r *RoutingKitCCHRunner) PreprocessDuration() time.Duration {
	return time.Duration(r.orderNS + r.topologyNS + r.customizeNS)
}
func (r *RoutingKitCCHRunner) Fingerprint() string { return r.fingerprint }

func (r *RoutingKitCCHRunner) Run(ctx context.Context, g *graph.Graph, source, target int) (search.Result, error) {
	if err := ctx.Err(); err != nil {
		return search.Result{}, err
	}
	if source < 0 || source >= len(g.Nodes) || target < 0 || target >= len(g.Nodes) {
		return search.Result{}, errors.New("maxsearch: source or target is out of range")
	}
	if g != r.graph {
		return search.Result{}, errors.New("maxsearch: RoutingKit CCH runner used with a different Aegis graph instance")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return search.Result{}, errors.New("maxsearch: RoutingKit CCH runner is closed")
	}

	// runPlan cancels its per-query context after a successful result. That
	// cancellation must not kill the persistent sidecar after the query already
	// completed. Only terminate the process when cancellation wins while the
	// blocking query is genuinely still outstanding.
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
	result, err := parseRoutingKitCCHResponse(line, len(g.Nodes))
	if err != nil {
		return search.Result{}, r.protocolError("parse query result", err)
	}
	if !result.Stats.Reachable {
		return search.Result{}, ErrRoutingKitCCHUnreachableUncertified
	}
	if !search.Validate(g, source, target, result) {
		return search.Result{}, errors.New("maxsearch: RoutingKit CCH returned an invalid path")
	}
	return result, nil
}

func (r *RoutingKitCCHRunner) Close() error {
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
			return r.protocolError("wait for RoutingKit CCH server", waitErr)
		}
		return waitErr
	}
	return nil
}

func (r *RoutingKitCCHRunner) protocolError(stage string, err error) error {
	message := strings.TrimSpace(r.stderr.String())
	switch {
	case err != nil && message != "":
		return fmt.Errorf("maxsearch: RoutingKit CCH %s: %s: %w", stage, message, err)
	case err != nil:
		return fmt.Errorf("maxsearch: RoutingKit CCH %s: %w", stage, err)
	case message != "":
		return fmt.Errorf("maxsearch: RoutingKit CCH %s: %s", stage, message)
	default:
		return fmt.Errorf("maxsearch: RoutingKit CCH %s", stage)
	}
}

func parsePositiveInt64(text string) (int64, error) {
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("duration must be positive")
	}
	return value, nil
}

func parseRoutingKitCCHResponse(line string, nodeCount int) (search.Result, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return search.Result{}, errors.New("short response")
	}
	switch fields[0] {
	case "U":
		if len(fields) != 2 {
			return search.Result{}, errors.New("invalid unreachable response")
		}
		duration, err := parsePositiveInt64(fields[1])
		if err != nil {
			return search.Result{}, errors.New("invalid duration")
		}
		return search.Result{Stats: search.Stats{Algorithm: RoutingKitCCH, DurationNS: duration, Reachable: false}}, nil
	case "R":
		if len(fields) < 5 {
			return search.Result{}, errors.New("invalid reachable response")
		}
		distance, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return search.Result{}, errors.New("invalid distance")
		}
		duration, err := parsePositiveInt64(fields[2])
		if err != nil {
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
				Algorithm:  RoutingKitCCH,
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
