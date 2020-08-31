package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

type startMsg struct{}
type stopMsg struct{}

type actorTimerCallback func()
type timerMsg struct {
	callback actorTimerCallback
	tid      timerID
}

type actorAddress struct {
	actorID  uint64
	nodeAddr net.TCPAddr
}

type callReq struct {
	callID uint32
	to     actorAddress
	from   actorAddress
	method string
	param  interface{}
}

type callRep struct {
	callID uint32
	to     actorAddress
	from   actorAddress
	value  interface{}
	err    error
}

type actorAgent interface {
	start(*actor)
	stop()
}

type actor struct {
	id      uint64
	mailbox chan interface{}
	agent   actorAgent

	// call
	lastCallID uint32
	sessions   map[uint32]callSession
}

type callSession struct {
	tid       timerID
	yieldChan chan yieldData
}

type yieldData struct {
	value interface{}
	err   error
}

const (
	secondsCallTimeout = 30
	capacityMailBox    = 100
)

func newActor(id uint64, agent actorAgent) *actor {
	return &actor{
		id:         id,
		mailbox:    make(chan interface{}, capacityMailBox),
		agent:      agent,
		lastCallID: 0,
		sessions:   make(map[uint32]callSession),
	}
}

func (a *actor) post(msg interface{}) {
	a.mailbox <- msg
}

func (a *actor) yield(yid uint32) yieldData {
	session := callSession{
		tid: a.registerTimer(secondsCallTimeout*1000, func() {
			a.resume(yid, nil, fmt.Errorf("call timeout"))
		}),
		yieldChan: make(chan yieldData, 1),
	}

	a.sessions[yid] = session

	go a.poll()

	return <-session.yieldChan
}

func (a *actor) resume(callID uint32, value interface{}, err error) {
	session, ok := a.sessions[callID]
	if ok {
		delete(a.sessions, callID)

		ydata := yieldData{value, err}
		session.yieldChan <- ydata
	}
}

func (a *actor) poll() {
	msg := <-a.mailbox

	// handle msg
	switch msg.(type) {
	case startMsg:
		a.agent.start(a)

	case stopMsg:
		a.agent.stop()

	case timerMsg:
		tmsg := msg.(*timerMsg)
		tnode := (*timerNode)(tmsg.tid)
		if tnode != nil && tnode.isValid() {
			tmsg.callback()
		}

	case callReq:
		req := msg.(*callReq)
		result, err := a.invokeAgent(req.method, req.param)
		rep := callRep{
			callID: req.callID,
			from:   req.to,
			to:     req.from,
			value:  result,
			err:    err,
		}

		// send reponse
		Gn.transferMsg(rep)

	case callRep:
		rep := msg.(*callRep)
		a.resume(rep.callID, rep.value, rep.err)
	}
}

func (a *actor) invokeAgent(method string, param interface{}) (interface{}, error) {
	inputs := make([]reflect.Value, 1)
	inputs[0] = reflect.ValueOf(param)
	outputs := reflect.ValueOf(a.agent).MethodByName(method).Call(inputs)
	if len(outputs) != 2 {
		return nil, fmt.Errorf("invokeAgent %s error: return value ill-format", method)
	}

	return outputs[0].Interface(), outputs[1].Interface().(error)
}

func (a *actor) registerTimer(millisecond uint32, callback actorTimerCallback) timerID {
	realcb := func(tid timerID) {
		msg := timerMsg{callback, tid}
		a.post(msg)
	}

	return Gn.registerTimer(millisecond, realcb)
}

func (a *actor) unregisterTimer(tid timerID) {
	Gn.unregisterTimer(tid)
}

func (a *actor) call(to uint64, method string, param interface{}) (interface{}, error) {
	a.lastCallID++
	req := callReq{
		callID: a.lastCallID,
		from:   actorAddress{a.id, net.TCPAddr{}},
		to:     actorAddress{to, net.TCPAddr{}},
		method: method,
		param:  param,
	}

	Gn.transferMsg(req)

	ydata := a.yield(a.lastCallID)
	return ydata.value, ydata.err
}

type gnode2 struct {
	done chan bool

	// actors
	amutex      sync.Mutex
	actors      map[uint64]*actor
	lastActorID uint64

	// timer manager
	currentTick uint32
	slots       [slotCount]timerNode
	tmutex      sync.Mutex
}

func (g *gnode2) transferMsg(msg interface{}) {
	switch msg.(type) {
	case callReq:
		req := msg.(callReq)

		// find in local actors
		{
			g.amutex.Lock()
			defer g.amutex.Unlock()
			a, ok := g.actors[req.to.actorID]
			if ok {
				a.post(msg)
			}
		}

	case callRep:
		rep := msg.(callRep)

		// find in local actors
		{
			g.amutex.Lock()
			defer g.amutex.Unlock()
			a, ok := g.actors[rep.to.actorID]
			if ok {
				a.post(msg)
			}
		}

		// TODO: remote find
	}
}

// Gn is the global node reference
var Gn *gnode2

type timerCallback func(timerID)

type timerNode struct {
	repeat   bool
	valid    uint32 // used as boolean
	ticks    uint32
	callback timerCallback

	next *timerNode
}

func (t *timerNode) isValid() bool {
	return (atomic.LoadUint32(&t.valid) == 0)
}

func (t *timerNode) setValid(valid bool) {
	if valid {
		atomic.StoreUint32(&t.valid, 1)
	} else {
		atomic.StoreUint32(&t.valid, 0)
	}
}

type timerID *timerNode

const (
	millisecPerTick = 100
	ticksOneDay     = 24 * 60 * 60 * 1000 / millisecPerTick
	slotCount       = ticksOneDay
)

func (g *gnode2) addNode(node *timerNode) {
	slotIndex := (atomic.LoadUint32(&g.currentTick) + node.ticks) / slotCount

	g.tmutex.Lock()
	defer g.tmutex.Unlock()
	oldNext := g.slots[slotIndex].next
	g.slots[slotIndex].next = node
	node.next = oldNext
}

func (g *gnode2) registerTimer(millisecond uint32, callback timerCallback) timerID {
	ticks := (millisecond + millisecPerTick - 1) / millisecPerTick
	newNode := &timerNode{
		repeat:   true,
		valid:    1,
		ticks:    ticks,
		callback: callback,
		next:     nil,
	}

	g.addNode(newNode)

	return newNode
}

func (g *gnode2) unregisterTimer(tid timerID) {
	if tid == nil {
		return
	}

	tnode := (*timerNode)(tid)
	tnode.setValid(false)
}

func (g *gnode2) tickLoop() {
	ticker := time.NewTicker(millisecPerTick * time.Millisecond)

	for {
		select {
		case <-g.done:
			return

		case <-ticker.C:
			var node *timerNode
			{
				g.tmutex.Lock()
				defer g.tmutex.Unlock()

				node = g.slots[g.currentTick].next
				g.slots[g.currentTick].next = nil
			}

			// Traverse timers
			for node != nil {
				next := node.next

				if node.isValid() {
					node.callback(node)
				}

				if node.isValid() && node.repeat {
					g.addNode(node)
				}

				node = next
			}

			atomic.AddUint32(&g.currentTick, 1)
		}
	}
}

func (g *gnode2) spawnActor(agent actorAgent) {
	var a *actor
	aid := atomic.AddUint64(&g.lastActorID, 1)

	{
		g.amutex.Lock()
		defer g.amutex.Unlock()

		a = newActor(aid, agent)
		g.actors[aid] = a
	}

	a.mailbox <- startMsg{}
	go a.poll()
}

type calculator struct {
	id uint64
	a  *actor
}

type sumParam struct {
	a, b int
}

func (c calculator) sum(param sumParam) int {
	sum := param.a + param.b
	fmt.Println("receive request: ", param.a, " + ", param.b)
	return sum
}

func (c calculator) start(a *actor) {
	c.a = a
	//	a.registerTimer(1000, c.onTimer)

	param := sumParam{1, 2}
	sum, err := a.call(2, "sum", param)

	if err != nil {
		fmt.Println("call sum fail: ", err)
	} else {
		fmt.Println(param.a, " + ", param.b, " = ", sum)
	}
}

func (c calculator) stop() {
}

func (c calculator) onTimer() {
	fmt.Println("second tick")
}

func main() {
	println("hello world")

	Gn = &gnode2{
		done:   make(chan bool),
		actors: make(map[uint64]*actor),
	}

	calc1 := calculator{1, nil}
	Gn.spawnActor(&calc1)

	input := bufio.NewScanner(os.Stdin)
	input.Scan()
}
