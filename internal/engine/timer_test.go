package engine

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestTimer(t *testing.T) {
	var q = NewTimeQueue()
	var tasks []*Task
	for i := 0; i < 10; i++ {
		timeS := rand.Int63n(100)
		fmt.Println(timeS, "=>", i)
		t := Task{TimeStamp: timeS, Event: Event{}}
		tasks = append(tasks, &t)
		q.PushTask(&t)
	}

	//remove 6
	fmt.Println("taskts:", tasks[6].Index, tasks[6].TimeStamp)
	q.DeleteTaskByIndex(tasks[6].Index, tasks[6].TimeStamp, 0)

	for q.Tq.Len() > 0 {
		t := q.PopTask()
		fmt.Println(t)
	}

}

func TestConcurrentTimer(t *testing.T) {
	var q = NewTimeQueue()
	var tasks []*Task
	for i := 0; i < 10; i++ {
		timeS := rand.Int63n(100)
		t := Task{TimeStamp: timeS, Event: Event{}}
		tasks = append(tasks, &t)
		q.PushTask(&t)
	}

	fmt.Println("taskts:", tasks[6].Index, tasks[6].TimeStamp)
	go func() {
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		fmt.Println("index changing:", tasks[6].Index)
		// q.DeleteTaskByIndex(6)
		// q.DeleteTaskByIndex(10)
		// q.DeleteTaskByIndex(12)
	}()

	for i := 0; i < 10; i++ {
		timeS := rand.Int63n(100)
		t := Task{TimeStamp: timeS, Event: Event{}}
		tasks = append(tasks, &t)
		q.PushTask(&t)
	}

	for q.Len() > 0 {
		t := q.PopTask()
		fmt.Println(t)
	}
	time.Sleep(1 * time.Second)
}
