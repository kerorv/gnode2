package gnode2

import (
	"fmt"
	"gnode2/base"
	"reflect"
	"time"
)

type startMsg struct{}
type stopMsg struct{}

type actorTimerCallback func()
type timerMsg struct {
	callback actorTimerCallback
	tid      base.TimerID
}

type actorAddress struct {
	id   uint64
	kind string
	node string
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

// ActorAgent actor agent
type ActorAgent interface {
	Start(*Actor)
	Stop()
}

// Actor actor
type Actor struct {
	id      uint64
	mailbox chan interface{}
	agent   ActorAgent

	// call
	lastCallID uint32
	sessions   map[uint32]callSession
}

type callSession struct {
	tid       base.TimerID
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

func newActor(id uint64, agent ActorAgent) *Actor {
	return &Actor{
		id:         id,
		mailbox:    make(chan interface{}, capacityMailBox),
		agent:      agent,
		lastCallID: 0,
		sessions:   make(map[uint32]callSession),
	}
}

func (a *Actor) post(msg interface{}) {
	a.mailbox <- msg
}

func (a *Actor) yield(yid uint32, timeout time.Duration) yieldData {
	session := callSession{
		tid: a.RegisterOneshotTimer(timeout,
			func() { a.resume(yid, nil, fmt.Errorf("call timeout")) }),
		yieldChan: make(chan yieldData, 1),
	}

	a.sessions[yid] = session

	go a.poll()

	data, ok := <-session.yieldChan
	if !ok {
		panic("yield channel is closed")
	}

	return data
}

func (a *Actor) resume(callID uint32, value interface{}, err error) {
	session, ok := a.sessions[callID]
	if ok {
		delete(a.sessions, callID)

		ydata := yieldData{value, err}
		session.yieldChan <- ydata
	}
}

func (a *Actor) onStart() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("[actor::onStart panic]", r)
		}
	}()

	a.agent.Start(a)
}

func (a *Actor) onStop() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("[actor::onStop panic]", r)
		}
	}()

	a.agent.Stop()
}

func (a *Actor) onTimer(callback actorTimerCallback) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("[actor::onTimer panic]", r)
		}
	}()

	callback()
}

func (a *Actor) onCallReq(req callReq) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("[actor::onCallReq panic]", r)
		}
	}()

	result, err := a.invokeAgent(req.method, req.param)
	rep := callRep{
		callID: req.callID,
		from:   req.to,
		to:     req.from,
		value:  result,
		err:    err,
	}
	Gn.transferMsg(rep)
}

func (a *Actor) poll() {
	for {
		msg, ok := <-a.mailbox
		if !ok {
			return
		}

		if msg == nil {
			continue
		}

		switch msg.(type) {
		case startMsg:
			a.onStart()

		case stopMsg:
			a.onStop()

		case timerMsg:
			tmsg := msg.(timerMsg)
			if base.IsTimerValid(tmsg.tid) {
				a.onTimer(tmsg.callback)
			}

		case callReq:
			req := msg.(callReq)
			a.onCallReq(req)

		case callRep:
			rep := msg.(callRep)
			a.resume(rep.callID, rep.value, rep.err)
			// finish this goroutine after resume
			return
		}
	}
}

func (a *Actor) invokeAgent(method string, param interface{}) (interface{}, error) {
	inputs := make([]reflect.Value, 1)
	inputs[0] = reflect.ValueOf(param)
	object := reflect.ValueOf(a.agent)
	m := object.MethodByName(method)
	outputs := m.Call(inputs)
	//outputs := reflect.ValueOf(a.agent).MethodByName(method).Call(inputs)
	if len(outputs) != 2 {
		return nil, fmt.Errorf("invokeAgent %s error: return value ill-format", method)
	}

	value := outputs[0].Interface()
	ierr := outputs[1].Interface()
	if ierr == nil {
		return value, nil
	}

	return value, ierr.(error)
}

// RegisterOneshotTimer regiester oneshot timer
func (a *Actor) RegisterOneshotTimer(delay time.Duration, callback actorTimerCallback) base.TimerID {
	realcb := func(tid base.TimerID) {
		msg := timerMsg{callback, tid}
		a.post(msg)
	}

	return Gn.tm.RegisterTimer(delay, realcb, false)
}

// RegisterRepeatTimer register repeat timer
func (a *Actor) RegisterRepeatTimer(interval time.Duration, callback actorTimerCallback) base.TimerID {
	realcb := func(tid base.TimerID) {
		msg := timerMsg{callback, tid}
		a.post(msg)
	}

	return Gn.tm.RegisterTimer(interval, realcb, true)
}

// UnregisterTimer unregister timer
func (a *Actor) UnregisterTimer(tid base.TimerID) {
	Gn.tm.UnregisterTimer(tid)
}

func (a *Actor) callTimeout(to uint64, method string, param interface{}, timeout time.Duration) (interface{}, error) {
	a.lastCallID++
	req := callReq{
		callID: a.lastCallID,
		from:   actorAddress{id: a.id},
		to:     actorAddress{id: to},
		method: method,
		param:  param,
	}

	Gn.transferMsg(req)

	ydata := a.yield(a.lastCallID, timeout)
	return ydata.value, ydata.err
}

func (a *Actor) call(to uint64, method string, param interface{}) (interface{}, error) {
	return a.call(to, method, secondsCallTimeout*1000)
}

// func (a *actor) queryAddress(targetID uint64, targetKind string) (string, error) {
// 	if addresss := Gn.transferMsg(targetID, targetKind) {
// 		return address, nil
// 	}

// 	a.lastCallID++
// 	type queryAddressParam struct {
// 		id   uint64
// 		kind string
// 	}
// 	param := queryAddressParam{targetID, targetKind}
// 	req := callReq{
// 		callID: a.lastCallID,
// 		from:   actorAddress{id: a.id},
// 		to:     actorAddress{id: target},
// 		method: "queryAddress",
// 		param:  param,
// 	}

// 	ydata := a.yield(a.lastCallID, timeout)
// 	if ydata.err != nil {
// 		return "", ydata.err
// 	}

// 	address, ok := ydata.value.(string)
// 	if !ok {
// 		return "", fmt.Errorf("yield data cant convert to string")
// 	}

// 	Gn.UpdateActorAddress(targetID, targetKind, address)
// 	return address, nil
// }
