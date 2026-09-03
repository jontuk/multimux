package panetext

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const (
	workerLimit  = 2
	agentTimeout = 90 * time.Second
)

type Result struct {
	Text      string `json:"text"`
	Processor string `json:"processor"`
	Model     string `json:"model"`
	Warning   string `json:"warning"`
}

type Cleaner struct {
	lookPath            func(string) (string, error)
	classify            func(context.Context, agent, promptChunk) (map[int]bool, error)
	timeout             time.Duration
	classificationSlots chan struct{}
}

func New() *Cleaner {
	return &Cleaner{
		lookPath:            exec.LookPath,
		classify:            runAgent,
		timeout:             agentTimeout,
		classificationSlots: make(chan struct{}, workerLimit),
	}
}

func rawResult(raw []byte, warning string) Result {
	return Result{
		Text:      string(raw),
		Processor: "raw",
		Warning:   warning,
	}
}

func (c *Cleaner) Clean(ctx context.Context, raw []byte) Result {
	chunks := buildChunks(raw)
	if len(chunks) == 0 {
		return rawResult(raw, "")
	}

	selected, ok := discoverAgent(c.lookPath)
	if !ok {
		return rawResult(raw, "Automatic cleanup unavailable. Showing raw pane text.")
	}

	type outcome struct {
		joins map[int]bool
		err   error
		ran   bool
	}
	outcomes := make([]outcome, len(chunks))
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int, len(chunks))
	for i := range chunks {
		jobs <- i
	}
	close(jobs)

	workerCount := min(workerLimit, len(chunks))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				if workCtx.Err() != nil {
					return
				}

				select {
				case c.classificationSlots <- struct{}{}:
				case <-workCtx.Done():
					return
				}
				if workCtx.Err() != nil {
					<-c.classificationSlots
					return
				}

				joins, err := func() (map[int]bool, error) {
					defer func() { <-c.classificationSlots }()
					chunkCtx, stop := context.WithTimeout(workCtx, c.timeout)
					defer stop()
					return c.classify(chunkCtx, selected, chunks[index])
				}()
				outcomes[index] = outcome{joins: joins, err: err, ran: true}
				if err != nil || joins == nil {
					cancel()
					return
				}
			}
		}()
	}
	workers.Wait()

	warning := fmt.Sprintf("Automatic cleanup failed with %s. Showing raw pane text.", displayAgent(selected))
	if workCtx.Err() != nil {
		return rawResult(raw, warning)
	}

	joins := make(map[int]bool)
	for _, result := range outcomes {
		if !result.ran || result.err != nil || result.joins == nil {
			return rawResult(raw, warning)
		}
		for id, join := range result.joins {
			if join {
				joins[id] = true
			}
		}
	}

	return Result{
		Text:      string(applyJoins(raw, joins)),
		Processor: selected.name,
		Model:     selected.model,
	}
}

func displayAgent(a agent) string {
	if a.name == "codex" {
		return "Codex"
	}
	return "Claude"
}
