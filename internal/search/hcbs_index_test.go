package search

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestHCBSIndexExactOnBidirectionalChain(t *testing.T) {
	graphPath, sidecarPath := writeChainHCBSSidecar(t)
	index, err := LoadHCBSIndex(sidecarPath, graphPath)
	if err != nil {
		t.Fatal(err)
	}
	prefix := []uint64{0, 2, 5, 10}
	for source := 0; source < 4; source++ {
		for target := 0; target < 4; target++ {
			distance, reachable, err := index.Distance(source, target)
			if err != nil {
				t.Fatalf("%d->%d: %v", source, target, err)
			}
			if !reachable {
				t.Fatalf("%d->%d unexpectedly unreachable", source, target)
			}
			want := prefix[target]
			if prefix[source] > want {
				want = prefix[source] - want
			} else {
				want -= prefix[source]
			}
			if distance != want {
				t.Fatalf("%d->%d distance=%d want=%d", source, target, distance, want)
			}
		}
	}
}

func TestHCBSIndexRejectsWrongGraphFingerprint(t *testing.T) {
	graphPath, sidecarPath := writeChainHCBSSidecar(t)
	if err := os.WriteFile(graphPath, []byte("different graph bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHCBSIndex(sidecarPath, graphPath); err == nil {
		t.Fatal("expected graph fingerprint mismatch")
	}
}

func TestHCBSIndexConcurrentQueries(t *testing.T) {
	graphPath, sidecarPath := writeChainHCBSSidecar(t)
	index, err := LoadHCBSIndex(sidecarPath, graphPath)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	const rounds = 500
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				source := (i + offset) % 4
				target := (3*i + offset + 1) % 4
				_, reachable, err := index.Distance(source, target)
				if err != nil {
					errs <- err
					return
				}
				if !reachable {
					errs <- errUnexpectedUnreachable{source: source, target: target}
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

type errUnexpectedUnreachable struct{ source, target int }

func (e errUnexpectedUnreachable) Error() string {
	return "HCBS concurrent query unexpectedly unreachable"
}

func writeChainHCBSSidecar(t *testing.T) (graphPath, sidecarPath string) {
	t.Helper()
	dir := t.TempDir()
	graphPath = filepath.Join(dir, "graph.aegis")
	graphBytes := []byte("hcbs-test-graph-fingerprint")
	if err := os.WriteFile(graphPath, graphBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(graphBytes)
	sidecarPath = filepath.Join(dir, "graph.hcbs")
	f, err := os.Create(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriter(f)
	if _, err := w.Write(hcbsSidecarMagic[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(digest[:]); err != nil {
		t.Fatal(err)
	}
	mustWriteBinary(t, w, uint32(4))
	mustWriteBinary(t, w, uint64(3))
	mustWriteBinary(t, w, uint64(3))
	mustWriteBinary(t, w, []uint32{0, 1, 2, 3})
	firstOut := []uint32{0, 1, 2, 3, 3}
	head := []uint32{1, 2, 3}
	weight := []uint32{2, 3, 5}
	for repeat := 0; repeat < 2; repeat++ {
		mustWriteBinary(t, w, firstOut)
		mustWriteBinary(t, w, head)
		mustWriteBinary(t, w, weight)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return graphPath, sidecarPath
}

func mustWriteBinary(t *testing.T, w *bufio.Writer, value any) {
	t.Helper()
	if err := binary.Write(w, binary.LittleEndian, value); err != nil {
		t.Fatal(err)
	}
}
