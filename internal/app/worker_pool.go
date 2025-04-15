package app

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// JobFunc represents a job to be executed by the worker pool
type JobFunc func()

// WorkerPool manages a pool of workers for concurrent execution
type WorkerPool struct {
	workQueue chan JobFunc
	wg        sync.WaitGroup
	once      sync.Once
	closed    atomic.Bool
}

var (
	// Global worker pool instance
	globalPool     *WorkerPool
	globalPoolOnce sync.Once
)

// GetGlobalPool returns the global worker pool instance
func GetGlobalPool() *WorkerPool {
	globalPoolOnce.Do(func() {
		// Default to CPUs/2 for number of workers, minimum 1
		numWorkers := 1
		if numWorkers < 1 {
			numWorkers = 1
		}

		// Allow override via flags if set
		if GetFlags().Threads > 0 {
			numWorkers = GetFlags().Threads
		}

		globalPool = NewWorkerPool(numWorkers)
		globalPool.Start()
	})
	return globalPool
}

// NewWorkerPool creates a new worker pool with the specified number of workers
func NewWorkerPool(numWorkers int) *WorkerPool {
	// Buffer the channel to allow more queueing (10x the worker count)
	return &WorkerPool{
		workQueue: make(chan JobFunc, numWorkers*10),
	}
}

// Start launches the worker pool
func (p *WorkerPool) Start() {
	p.once.Do(func() {
		numWorkers := cap(p.workQueue) / 10
		p.wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			go func(workerID int) {
				defer p.wg.Done()
				if GetFlags().Debug {
					LogInfo("Worker %d started", workerID)
				}
				for job := range p.workQueue {
					if job != nil {
						if GetFlags().Debug {
							LogInfo("Worker %d processing job", workerID)
						}
						job()
						if GetFlags().Debug {
							LogInfo("Worker %d completed job", workerID)
						}
					}
				}
				if GetFlags().Debug {
					LogInfo("Worker %d shutting down", workerID)
				}
			}(i)
		}

		if GetFlags().Debug {
			LogInfo("Started worker pool with %d workers", numWorkers)
		}
	})
}

// Submit adds a job to the worker pool
func (p *WorkerPool) Submit(job JobFunc) {
	if p.closed.Load() {
		// If pool is closed, execute job directly
		job()
		return
	}

	if GetFlags().Debug {
		LogInfo("Submitting job to worker pool (queue length: %d/%d)", len(p.workQueue), cap(p.workQueue))
	}

	select {
	case p.workQueue <- job:
		// Job submitted successfully
		if GetFlags().Debug {
			LogInfo("Job submitted successfully")
		}
	default:
		// Channel is full, execute job directly
		if GetFlags().Debug {
			LogInfo("Worker pool queue full, executing job directly")
		}
		job()
	}
}

// Wait waits for all jobs to complete
func (p *WorkerPool) Wait() {
	if p.closed.Load() {
		return
	}
	p.closed.Store(true)
	close(p.workQueue)
	p.wg.Wait()
}

// Printf formats and prints a message
func Printf(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

// LogInfo logs informational messages if debug mode is enabled
func LogInfo(format string, args ...interface{}) {
	if GetFlags().Debug {
		Printf(format, args...)
	}
}
