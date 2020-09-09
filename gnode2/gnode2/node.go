package gnode2

import (
	"fmt"
	"gnode2/base"
	"sync"
	"sync/atomic"
)

type node struct {
	done chan bool

	// tick timer
	tm base.TimerManager

	// actors
	amutex      sync.Mutex
	actors      map[uint64]*Actor
	lastActorID uint64
}

func (g *node) transferMsg(msg interface{}) {
	switch msg.(type) {
	case callReq:
		req := msg.(callReq)

		// find in local actors
		find := func() bool {
			g.amutex.Lock()
			defer g.amutex.Unlock()
			a, ok := g.actors[req.to.id]
			if ok {
				a.post(msg)
			}

			return ok
		}()

		if find {
			return
		}

		// TODO: query actor in meta server

	case callRep:
		rep := msg.(callRep)

		// find in local actors
		func() {
			g.amutex.Lock()
			defer g.amutex.Unlock()
			a, ok := g.actors[rep.to.id]
			if ok {
				a.post(msg)
			}
		}()

		// TODO: query actor in meta server
	}
}

// Gn is the global node reference
var Gn *node

func (g *node) spawnActor(agent ActorAgent) {
	var a *Actor
	aid := atomic.AddUint64(&g.lastActorID, 1)

	func() {
		g.amutex.Lock()
		defer g.amutex.Unlock()

		a = newActor(aid, agent)
		g.actors[aid] = a
	}()

	a.mailbox <- startMsg{}
	go a.poll()
}

func (g *node) OnMessage(msg interface{}) {
	fmt.Println(msg)
}

// StartGnode start gnode
func StartGnode() {
	Gn = &node{
		done:   make(chan bool),
		actors: make(map[uint64]*Actor),
	}

	Gn.tm.Run()
}

// SpawnActor spawn actor
func SpawnActor(agent ActorAgent) {
	Gn.spawnActor(agent)
}
