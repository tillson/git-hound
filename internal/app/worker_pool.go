package app

import (
	"fmt"
	"sync"
)

// JobFunc represents a job to be executed by the worker pool
type JobFunc func()

// WorkerPool manages a pool of workers for concurrent execution
type WorkerPool struct {
	workQueue chan JobFunc
	wg        sync.WaitGroup
	once      sync.Once
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
	// Buffer the channel to allow some queueing (3x the worker count)
	return &WorkerPool{
		workQueue: make(chan JobFunc, numWorkers*3),
	}
}

// Start launches the worker pool
func (p *WorkerPool) Start() {
	p.once.Do(func() {
		numWorkers := cap(p.workQueue) / 3
		p.wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			go func() {
				defer p.wg.Done()
				for job := range p.workQueue {
					if job != nil {
						job()
					}
				}
			}()
		}

		if GetFlags().Debug {
			LogInfo("Started worker pool with %d workers", numWorkers)
		}
	})
}

// Submit adds a job to the worker pool
func (p *WorkerPool) Submit(job JobFunc) {
	p.workQueue <- job
}

// Wait waits for all jobs to complete
func (p *WorkerPool) Wait() {
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
