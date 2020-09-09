package base

import (
	"container/list"
	"sync"
)

// ConcurrentQueue is a concurrent queue
type ConcurrentQueue struct {
	cflag bool // close flag
	m     sync.Mutex
	l     *list.List
	c     *sync.Cond
}

// NewConqueue create new ConcurrentQueue
func NewConqueue() *ConcurrentQueue {
	q := &ConcurrentQueue{
		cflag: false,
		l:     &list.List{},
	}

	q.c = sync.NewCond(&q.m)
	return q
}

// Put put item to the tail of queue
func (q *ConcurrentQueue) Put(item interface{}) {
	q.c.L.Lock()
	q.l.PushBack(item)
	q.c.L.Unlock()

	q.c.Signal()
}

// Wait wait for item, will block queue if queue is empty
// pop the first item if queue has any item
func (q *ConcurrentQueue) Wait() interface{} {
	q.c.L.Lock()
	defer q.c.L.Unlock()

	if q.l.Len() == 0 && !q.cflag {
		q.c.Wait()
	}

	if q.cflag {
		return nil
	}

	e := q.l.Front()
	return q.l.Remove(e)
}

// Close close queue and notice all consumers
func (q *ConcurrentQueue) Close() {
	q.c.L.Lock()
	q.cflag = true
	q.c.L.Unlock()

	q.c.Broadcast()
}

// IsClose return queue is closed
func (q *ConcurrentQueue) IsClose() bool {
	q.c.L.Lock()
	defer q.c.L.Unlock()

	return q.cflag
}

// Len return count of items in queue
func (q *ConcurrentQueue) Len() int {
	q.c.L.Lock()
	defer q.c.L.Unlock()

	return q.l.Len()
}

// Clear clear queue
func (q *ConcurrentQueue) Clear() {
	q.c.L.Lock()
	defer q.c.L.Unlock()

	q.l.Init()
}
