package panetext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCleanerNewUsesProductionDefaults(t *testing.T) {
	cleaner := New()
	if cleaner.lookPath == nil {
		t.Fatal("New returned a Cleaner with no executable lookup")
	}
	if cleaner.classify == nil {
		t.Fatal("New returned a Cleaner with no classifier")
	}
	if cleaner.timeout != agentTimeout {
		t.Fatalf("New timeout = %v, want %v", cleaner.timeout, agentTimeout)
	}
	if workerLimit != 2 {
		t.Fatalf("workerLimit = %d, want 2", workerLimit)
	}

	got, err := json.Marshal(Result{Text: "text", Processor: "codex", Model: "model", Warning: "warning"})
	if err != nil {
		t.Fatalf("marshal Result: %v", err)
	}
	if want := `{"text":"text","processor":"codex","model":"model","warning":"warning"}`; string(got) != want {
		t.Fatalf("marshaled Result = %s, want %s", got, want)
	}
}

func TestCleanerJoinsOnlyClassifiedBoundaries(t *testing.T) {
	raw := []byte("wrapped prose\ncontinues\n\n- item\ncode\n")
	type observation struct {
		agent        agent
		ownsBoundary bool
	}
	observed := make(chan observation, 1)
	cleaner := testCleaner(t, "codex", func(_ context.Context, gotAgent agent, chunk promptChunk) (map[int]bool, error) {
		_, ownsBoundary := chunk.ids[0]
		observed <- observation{agent: gotAgent, ownsBoundary: ownsBoundary}
		return map[int]bool{0: true}, nil
	})

	got := cleaner.Clean(context.Background(), raw)
	var classifierObservation observation
	select {
	case classifierObservation = <-observed:
	case <-time.After(time.Second):
		t.Fatal("classifier did not report its observation")
	}
	if classifierObservation.agent.name != "codex" || classifierObservation.agent.model != "gpt-5.6-luna" {
		t.Fatalf("agent = %#v, want Codex with gpt-5.6-luna", classifierObservation.agent)
	}
	if !classifierObservation.ownsBoundary {
		t.Fatal("classifier chunk does not own boundary 0")
	}
	want := Result{
		Text:      "wrapped prose continues\n\n- item\ncode\n",
		Processor: "codex",
		Model:     "gpt-5.6-luna",
	}
	if got != want {
		t.Fatalf("Clean() = %#v, want %#v", got, want)
	}
}

func TestCleanerSkipsDiscoveryWithoutEligibleBoundaries(t *testing.T) {
	raw := []byte("one line\n\n")
	discoveryCalled := false
	var classifierCalled atomic.Bool
	cleaner := testCleaner(t, "codex", func(context.Context, agent, promptChunk) (map[int]bool, error) {
		classifierCalled.Store(true)
		return nil, nil
	})
	cleaner.lookPath = func(string) (string, error) {
		discoveryCalled = true
		return "/fake/codex", nil
	}

	got := cleaner.Clean(context.Background(), raw)
	if discoveryCalled {
		t.Fatal("Clean called executable discovery without eligible boundaries")
	}
	if classifierCalled.Load() {
		t.Fatal("Clean called classifier without eligible boundaries")
	}
	if want := (Result{Text: string(raw), Processor: "raw"}); got != want {
		t.Fatalf("Clean() = %#v, want %#v", got, want)
	}
}

func TestCleanerReturnsByteExactRawWhenNoAgentExists(t *testing.T) {
	raw := []byte{'b', 'e', 'f', 'o', 'r', 'e', 0xff, '\n', 'a', 'f', 't', 'e', 'r', '\n'}
	var classifierCalled atomic.Bool
	cleaner := testCleaner(t, "", func(context.Context, agent, promptChunk) (map[int]bool, error) {
		classifierCalled.Store(true)
		return nil, nil
	})

	got := cleaner.Clean(context.Background(), raw)
	if classifierCalled.Load() {
		t.Fatal("Clean called classifier without an installed agent")
	}
	if !reflect.DeepEqual([]byte(got.Text), raw) {
		t.Fatalf("Clean text bytes = %v, want %v", []byte(got.Text), raw)
	}
	if got.Processor != "raw" || got.Model != "" {
		t.Fatalf("fallback metadata = processor %q, model %q", got.Processor, got.Model)
	}
	if got.Warning != "Automatic cleanup unavailable. Showing raw pane text." {
		t.Fatalf("warning = %q, want unavailable fallback warning", got.Warning)
	}
}

func TestCleanerChunkFailureReturnsWholeRawSnapshot(t *testing.T) {
	raw := multiChunkRaw(maxSamplesPerChunk + 2)
	var calls atomic.Int32
	cleaner := testCleaner(t, "claude", func(_ context.Context, _ agent, chunk promptChunk) (map[int]bool, error) {
		calls.Add(1)
		if _, firstChunk := chunk.ids[0]; firstChunk {
			return map[int]bool{0: true}, nil
		}
		return nil, errors.New("classification failed")
	})

	got := cleaner.Clean(context.Background(), raw)
	want := rawResult(raw, "Automatic cleanup failed with Claude. Showing raw pane text.")
	if got != want {
		t.Fatalf("Clean() = %#v, want atomic fallback %#v", got, want)
	}
	if calls.Load() < 2 {
		t.Fatalf("classifier calls = %d, want multiple chunks exercised", calls.Load())
	}
}

func TestCleanerChunkFailureCancelsSiblingAndWaitsForIt(t *testing.T) {
	raw := multiChunkRaw(maxSamplesPerChunk + 2)
	failingStarted := make(chan struct{})
	siblingStarted := make(chan struct{})
	releaseFailure := make(chan struct{})
	siblingCanceled := make(chan struct{})
	cleaner := testCleaner(t, "codex", func(ctx context.Context, _ agent, chunk promptChunk) (map[int]bool, error) {
		if _, firstChunk := chunk.ids[0]; firstChunk {
			close(failingStarted)
			<-releaseFailure
			return nil, errors.New("classification failed")
		}

		close(siblingStarted)
		<-ctx.Done()
		close(siblingCanceled)
		return nil, ctx.Err()
	})

	result := make(chan Result, 1)
	go func() {
		result <- cleaner.Clean(context.Background(), raw)
	}()
	waitForSignal(t, failingStarted, "failing chunk to start")
	waitForSignal(t, siblingStarted, "sibling chunk to start")
	close(releaseFailure)

	var got Result
	select {
	case got = <-result:
	case <-time.After(time.Second):
		t.Fatal("Clean did not return after chunk failure")
	}
	select {
	case <-siblingCanceled:
	default:
		t.Fatal("Clean returned before canceled sibling finished")
	}
	want := rawResult(raw, "Automatic cleanup failed with Codex. Showing raw pane text.")
	if got != want {
		t.Fatalf("Clean() = %#v, want atomic fallback %#v", got, want)
	}
}

func TestCleanerChunkFailureCancelsSiblingAndSkipsQueuedChunk(t *testing.T) {
	raw := multiChunkRaw(maxSamplesPerChunk*2 + 2)
	if got := len(buildChunks(raw)); got != 3 {
		t.Fatalf("buildChunks returned %d chunks, want exactly 3", got)
	}
	failingStarted := make(chan struct{})
	siblingStarted := make(chan struct{})
	releaseFailure := make(chan struct{})
	siblingCanceled := make(chan struct{})
	releaseSibling := make(chan struct{})
	thirdStarted := make(chan struct{})
	cleaner := testCleaner(t, "claude", func(ctx context.Context, _ agent, chunk promptChunk) (map[int]bool, error) {
		if _, firstChunk := chunk.ids[0]; firstChunk {
			close(failingStarted)
			<-releaseFailure
			return map[int]bool{0: true}, errors.New("classification failed")
		}
		if _, secondChunk := chunk.ids[maxSamplesPerChunk]; secondChunk {
			close(siblingStarted)
			<-ctx.Done()
			close(siblingCanceled)
			<-releaseSibling
			return nil, ctx.Err()
		}

		close(thirdStarted)
		return map[int]bool{}, nil
	})

	result := make(chan Result, 1)
	go func() {
		result <- cleaner.Clean(context.Background(), raw)
	}()
	waitForSignal(t, failingStarted, "failing chunk to start")
	waitForSignal(t, siblingStarted, "sibling chunk to start")
	select {
	case <-thirdStarted:
		t.Fatal("third queued chunk started while the first two were blocked")
	default:
	}
	close(releaseFailure)

	waitForSignal(t, siblingCanceled, "sibling to observe cancellation")
	var got Result
	returnedEarly := false
	select {
	case got = <-result:
		returnedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	thirdRan := false
	select {
	case <-thirdStarted:
		thirdRan = true
	default:
	}
	close(releaseSibling)
	if returnedEarly {
		t.Fatal("Clean returned while canceled sibling was still blocked")
	}
	if thirdRan {
		t.Fatal("third queued chunk started after sibling cancellation")
	}

	select {
	case got = <-result:
	case <-time.After(time.Second):
		t.Fatal("Clean did not return after canceled sibling finished")
	}
	select {
	case <-thirdStarted:
		t.Fatal("third queued chunk started after Clean returned")
	default:
	}
	if !bytes.Equal([]byte(got.Text), raw) {
		t.Fatalf("Clean text bytes differ from raw snapshot\n got: %v\nwant: %v", []byte(got.Text), raw)
	}
	if got.Processor != "raw" || got.Model != "" || got.Warning != "Automatic cleanup failed with Claude. Showing raw pane text." {
		t.Fatalf("Clean fallback metadata = %#v, want exact Claude raw fallback", got)
	}
}

func TestCleanerLimitsConcurrentChunks(t *testing.T) {
	raw := multiChunkRaw(maxSamplesPerChunk*2 + 2)
	release := make(chan struct{})
	started := make(chan struct{}, 3)
	var active atomic.Int32
	var maximum atomic.Int32
	cleaner := testCleaner(t, "codex", func(context.Context, agent, promptChunk) (map[int]bool, error) {
		current := active.Add(1)
		for {
			prior := maximum.Load()
			if current <= prior || maximum.CompareAndSwap(prior, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return map[int]bool{}, nil
	})

	result := make(chan Result, 1)
	go func() {
		result <- cleaner.Clean(context.Background(), raw)
	}()
	waitForSignal(t, started, "first chunk to start")
	waitForSignal(t, started, "second chunk to start")
	select {
	case <-started:
		t.Fatal("third chunk started while two classifiers were blocked")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	select {
	case got := <-result:
		if got.Processor != "codex" || got.Warning != "" {
			t.Fatalf("Clean() = %#v, want successful Codex result", got)
		}
	case <-time.After(time.Second):
		t.Fatal("Clean did not finish after releasing classifiers")
	}
	if got := maximum.Load(); got != workerLimit {
		t.Fatalf("maximum concurrent classifiers = %d, want %d", got, workerLimit)
	}
}

func TestCleanerUnionsSuccessfulChunkJoins(t *testing.T) {
	raw := multiChunkRaw(maxSamplesPerChunk + 2)
	cleaner := testCleaner(t, "claude", func(_ context.Context, _ agent, chunk promptChunk) (map[int]bool, error) {
		joins := make(map[int]bool)
		if _, ok := chunk.ids[0]; ok {
			joins[0] = true
		}
		if _, ok := chunk.ids[maxSamplesPerChunk]; ok {
			joins[maxSamplesPerChunk] = true
		}
		return joins, nil
	})

	got := cleaner.Clean(context.Background(), raw)
	wantText := string(applyJoins(raw, map[int]bool{0: true, maxSamplesPerChunk: true}))
	want := Result{Text: wantText, Processor: "claude", Model: "sonnet-5"}
	if got != want {
		t.Fatalf("Clean() = %#v, want union result %#v", got, want)
	}
}

func TestCleanerTreatsNilChunkOutcomeAsFailure(t *testing.T) {
	raw := []byte("wrapped\nprose\n")
	cleaner := testCleaner(t, "codex", func(context.Context, agent, promptChunk) (map[int]bool, error) {
		return nil, nil
	})

	got := cleaner.Clean(context.Background(), raw)
	want := rawResult(raw, "Automatic cleanup failed with Codex. Showing raw pane text.")
	if got != want {
		t.Fatalf("Clean() = %#v, want nil-outcome fallback %#v", got, want)
	}
}

func TestCleanerTimesOutChunkClassification(t *testing.T) {
	raw := []byte("wrapped\nprose\n")
	observed := make(chan error, 1)
	cleaner := testCleaner(t, "codex", func(ctx context.Context, _ agent, _ promptChunk) (map[int]bool, error) {
		_, hasDeadline := ctx.Deadline()
		if !hasDeadline {
			err := errors.New("classifier context has no deadline")
			observed <- err
			return nil, err
		}
		<-ctx.Done()
		observed <- ctx.Err()
		return nil, ctx.Err()
	})
	cleaner.timeout = 20 * time.Millisecond

	result := make(chan Result, 1)
	go func() {
		result <- cleaner.Clean(context.Background(), raw)
	}()
	var got Result
	select {
	case got = <-result:
	case <-time.After(time.Second):
		t.Fatal("Clean did not return after classifier timeout")
	}
	select {
	case err := <-observed:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("classifier context error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("classifier did not report timeout observation")
	}
	want := rawResult(raw, "Automatic cleanup failed with Codex. Showing raw pane text.")
	if got != want {
		t.Fatalf("Clean() = %#v, want timeout fallback %#v", got, want)
	}
}

func TestCleanerPropagatesRequestCancellation(t *testing.T) {
	raw := multiChunkRaw(maxSamplesPerChunk + 2)
	started := make(chan struct{}, 2)
	canceled := make(chan error, 2)
	cleaner := testCleaner(t, "claude", func(ctx context.Context, _ agent, _ promptChunk) (map[int]bool, error) {
		started <- struct{}{}
		<-ctx.Done()
		canceled <- ctx.Err()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan Result, 1)
	go func() {
		result <- cleaner.Clean(ctx, raw)
	}()
	waitForSignal(t, started, "first chunk to start")
	waitForSignal(t, started, "second chunk to start")
	cancel()

	for range 2 {
		select {
		case err := <-canceled:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("classifier context error = %v, want canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("classifier did not observe request cancellation")
		}
	}
	select {
	case got := <-result:
		want := rawResult(raw, "Automatic cleanup failed with Claude. Showing raw pane text.")
		if got != want {
			t.Fatalf("Clean() = %#v, want request-cancellation fallback %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("Clean did not finish after request cancellation")
	}
}

func testCleaner(t *testing.T, provider string, classify func(context.Context, agent, promptChunk) (map[int]bool, error)) *Cleaner {
	t.Helper()
	return &Cleaner{
		lookPath: func(name string) (string, error) {
			if name == provider {
				return "/fake/" + name, nil
			}
			return "", os.ErrNotExist
		},
		classify: classify,
		timeout:  time.Second,
	}
}

func multiChunkRaw(boundaries int) []byte {
	lines := make([]string, boundaries+1)
	for i := range lines {
		lines[i] = strings.Repeat("x", 8)
	}
	return []byte(strings.Join(lines, "\n"))
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
