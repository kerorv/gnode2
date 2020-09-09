package base

import (
	"sync"
	"sync/atomic"
	"time"
)

type timerCallback func(TimerID)

type timerNode struct {
	repeat   bool
	valid    uint32 // used as boolean
	ticks    uint32
	callback timerCallback

	next *timerNode
}

func (t *timerNode) isValid() bool {
	return (atomic.LoadUint32(&t.valid) != 0)
}

func (t *timerNode) setValid(valid bool) {
	if valid {
		atomic.StoreUint32(&t.valid, 1)
	} else {
		atomic.StoreUint32(&t.valid, 0)
	}
}

// TimerID is id of timer
type TimerID *timerNode

// IsTimerValid return whether timer is valid
func IsTimerValid(tid TimerID) bool {
	tnode := (*timerNode)(tid)
	if tnode == nil {
		return false
	}

	return tnode.isValid()
}

const (
	millisecPerTick = 100
	ticksOneDay     = 24 * 60 * 60 * 1000 / millisecPerTick
	slotCount       = ticksOneDay
)

// TimerManager is tick timer manager
type TimerManager struct {
	currentTick uint32
	done        chan interface{}

	sync.Mutex
	slots [slotCount]timerNode
}

func (tm *TimerManager) addNode(node *timerNode) {
	slotIndex := (atomic.LoadUint32(&tm.currentTick) + node.ticks) % slotCount

	tm.Lock()
	defer tm.Unlock()
	oldNext := tm.slots[slotIndex].next
	tm.slots[slotIndex].next = node
	node.next = oldNext
}

// Run start tick
func (tm *TimerManager) Run() {
	go tm.tickLoop()
}

// Stop stop tick
func (tm *TimerManager) Stop() {
	tm.done <- nil
}

// RegisterTimer register timer
func (tm *TimerManager) RegisterTimer(interval time.Duration, callback timerCallback, repeat bool) TimerID {
	ticks := (interval.Milliseconds() + (millisecPerTick - 1)) / millisecPerTick
	newNode := &timerNode{
		repeat:   repeat,
		valid:    1,
		ticks:    uint32(ticks),
		callback: callback,
		next:     nil,
	}

	tm.addNode(newNode)
	return newNode
}

// UnregisterTimer unregister timer
func (tm *TimerManager) UnregisterTimer(tid TimerID) {
	if tid == nil {
		return
	}

	tnode := (*timerNode)(tid)
	tnode.setValid(false)
}

func (tm *TimerManager) tickLoop() {
	ticker := time.NewTicker(millisecPerTick * time.Millisecond)

	for {
		select {
		case <-tm.done:
			return

		case <-ticker.C:
			var node *timerNode

			nextSlotIndex := atomic.AddUint32(&tm.currentTick, 1) % slotCount

			func() {
				tm.Lock()
				defer tm.Unlock()

				node = tm.slots[nextSlotIndex].next
				tm.slots[nextSlotIndex].next = nil
			}()

			// Traverse timers
			for node != nil {
				next := node.next

				if node.isValid() {
					node.callback(node)
				}

				if node.isValid() && node.repeat {
					tm.addNode(node)
				}

				node = next
			}
		}
	}
}
