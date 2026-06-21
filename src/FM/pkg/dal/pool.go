package dpuabstraction

import (
	"context"
	"fmt"
	"sync"

	gm "github.com/dashfabric/fm/pkg/gm"
)

// PluginPoolImpl manages worker pools for dispatching goals to plugins
type PluginPoolImpl struct {
	mu            sync.RWMutex
	dispatcher    PluginDispatcher
	workers       int
	jobQueue      chan *goalJob
	resultChans   map[string]chan *ProgramResult
	resultChanMu  sync.RWMutex
	running       bool
	cancel        context.CancelFunc
	ctx           context.Context
}

// goalJob wraps a goal state with its result channel
type goalJob struct {
	goal       *gm.PerENIGoalState
	resultChan chan *ProgramResult
}

// NewPluginPool creates a new plugin pool with specified worker count
func NewPluginPool(dispatcher PluginDispatcher, workers int) PluginPool {
	if workers < 1 {
		workers = 4
	}
	return &PluginPoolImpl{
		dispatcher:   dispatcher,
		workers:      workers,
		jobQueue:     make(chan *goalJob, workers*2),
		resultChans:  make(map[string]chan *ProgramResult),
	}
}

// Start initializes and starts the worker pool
func (p *PluginPoolImpl) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin pool already running")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.running = true

	// Start worker goroutines
	for i := 0; i < p.workers; i++ {
		go p.worker()
	}

	return nil
}

// Shutdown gracefully shuts down the worker pool
func (p *PluginPoolImpl) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return fmt.Errorf("plugin pool not running")
	}

	if p.cancel != nil {
		p.cancel()
	}
	close(p.jobQueue)
	p.running = false

	return nil
}

// Submit enqueues a goal for processing and returns a result channel
func (p *PluginPoolImpl) Submit(ctx context.Context, goal *gm.PerENIGoalState) <-chan *ProgramResult {
	p.mu.RLock()
	if !p.running {
		p.mu.RUnlock()
		// Return closed channel for error case
		ch := make(chan *ProgramResult)
		close(ch)
		return ch
	}
	p.mu.RUnlock()

	if goal == nil {
		ch := make(chan *ProgramResult)
		close(ch)
		return ch
	}

	select {
	case <-ctx.Done():
		ch := make(chan *ProgramResult)
		close(ch)
		return ch
	case <-p.ctx.Done():
		ch := make(chan *ProgramResult)
		close(ch)
		return ch
	default:
	}

	resultChan := make(chan *ProgramResult, 1)
	job := &goalJob{
		goal:       goal,
		resultChan: resultChan,
	}

	select {
	case p.jobQueue <- job:
		return resultChan
	case <-p.ctx.Done():
		close(resultChan)
		return resultChan
	}
}

	// worker processes jobs from the queue
func (p *PluginPoolImpl) worker() {
	for job := range p.jobQueue {
		if job == nil {
			continue
		}

		result, err := p.dispatcher.Dispatch(p.ctx, job.goal)
		if err != nil {
			result = &ProgramResult{
				ENI_ID:  job.goal.ENI_ID,
				Success: false,
				Error:   err.Error(),
			}
		}

		select {
		case job.resultChan <- result:
		case <-p.ctx.Done():
			close(job.resultChan)
			return
		}
	}
}
