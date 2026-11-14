package utils

type Job func()

type Queue struct {
	workers int
	jobs    chan Job
}

func NewQueue(workers int) *Queue {
	return &Queue{
		workers: workers,
		jobs:    make(chan Job, workers*10),
	}
}

func (q *Queue) Start() {
	for i := 0; i < q.workers; i++ {
		go func() {
			for job := range q.jobs {
				job()
			}
		}()
	}
}

func (q *Queue) Push(job Job) {
	q.jobs <- job
}
