package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"gnode2/gnode2"
)

type calculator struct {
	id   uint64
	tick uint64
	a    *gnode2.Actor
}

type sumParam struct {
	a, b int
}

func (c *calculator) Sum(param sumParam) (int, error) {
	sum := param.a + param.b
	fmt.Println("receive request: ", param.a, " + ", param.b)
	return sum, nil
}

func (c *calculator) Start(a *gnode2.Actor) {
	c.a = a

	// a.registerOneshotTimer(1000*time.Millisecond, c.onTimer)
	a.RegisterRepeatTimer(1000*time.Millisecond, c.onTimer)

	// if c.a.id == 1 {
	// 	param := sumParam{1, 2}
	// 	sum, err := a.call(2, "Sum", param)

	// 	if err != nil {
	// 		fmt.Println("call Sum fail: ", err)
	// 	} else {
	// 		fmt.Println(param.a, " + ", param.b, " = ", sum)
	// 	}
	// }
}

func (c *calculator) Stop() {
}

func (c *calculator) onTimer() {
	c.tick++
	fmt.Println("second tick", c.tick)
}

func main() {
	println("hello world")
	gnode2.StartGnode()

	calc1 := calculator{1, 0, nil}
	calc2 := calculator{2, 0, nil}
	gnode2.SpawnActor(&calc1)
	gnode2.SpawnActor(&calc2)

	// q := base.NewConqueue()
	// q.Put("123")
	// item := q.Wait()
	// fmt.Println(item)

	// hub := nethub.NewInternalHub(nethub.CreateProtomsgCodec)
	// hub.Start("localhost:6035")

	// hub.SendTo("127.0.0.1:6036", "message")

	input := bufio.NewScanner(os.Stdin)
	input.Scan()
}
