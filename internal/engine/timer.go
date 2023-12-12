package engine

import (
	"container/heap"
	"sync"
	"time"
)

const (
	INIT_DELY        = 30
	MAX_TICK_SECONDS = 30 * time.Second
)

type TimeQueue struct {
	sync.RWMutex
	Timer *time.Timer
	Tq    *TaskQueue
}

type Task struct {
	Index     int
	TimeStamp int64
	Event     Event
}

type TaskQueue []*Task

func (tq TaskQueue) Len() int {
	return len(tq)
}

func (tq TaskQueue) Less(i, j int) bool {
	return tq[i].TimeStamp < tq[j].TimeStamp
}

func (tq TaskQueue) Swap(i, j int) {
	tq[i], tq[j] = tq[j], tq[i]
	tq[i].Index = i
	tq[j].Index = j
}

func (tq *TaskQueue) Push(x any) {
	n := len(*tq)
	task := x.(*Task)
	task.Index = n
	*tq = append(*tq, task)
}

func (tq *TaskQueue) Pop() any {
	old := *tq
	n := len(old)
	task := old[n-1]
	old[n-1] = nil  // avoid memory leak
	task.Index = -1 // for safety
	*tq = old[0 : n-1]
	return task
}

func NewTimeQueue() *TimeQueue {
	q := make(TaskQueue, 0)
	heap.Init(&q)
	return &TimeQueue{
		Tq:    &q,
		Timer: time.NewTimer(INIT_DELY * time.Second),
	}
}

func (q *TimeQueue) PushTask(task *Task) {
	q.Lock()
	defer q.Unlock()
	heap.Push(q.Tq, task)
}

func (q *TimeQueue) PushTaskAndTick(task *Task) {
	q.Lock()
	defer q.Unlock()
	heap.Push(q.Tq, task)
	if len(*q.Tq) != 0 {
		task := (*q.Tq)[0]
		timeNow := time.Now().UnixNano()
		duration := time.Duration(task.TimeStamp - timeNow)
		if duration < 0 {
			duration = 1
		}
		q.Timer.Reset(1)
	}
}

func (q *TimeQueue) PopTask() *Task {
	q.Lock()
	defer q.Unlock()
	if q.Tq.Len() == 0 {
		return nil
	}
	return heap.Pop(q.Tq).(*Task)
}
func (q *TimeQueue) Peek() *Task {
	q.RLock()
	defer q.RUnlock()
	if len(*q.Tq) != 0 {
		return (*q.Tq)[0]
	}
	return nil
}

// func (q *TimeQueue) PopTasks(events []Event) int {
// 	q.Lock()
// 	defer q.Unlock()
// 	maxLen := len(events)
// 	count := 0
// 	timeNow := time.Now().UnixNano()
// 	for i := 0; i < maxLen; i++ {
// 		if len(*q.Tq) != 0 {
// 			if timeNow > (*q.Tq)[0].TimeStamp {
// 				events[i] = heap.Pop(q.Tq).(*Task).Event
// 				count++
// 			}
// 		} else {
// 			break
// 		}
// 	}
// 	return count
// }

// WARNING:
// the original implementation will run into race condition
//
//	{
//		if i == -1 {
//			return
//		}
//		heap.Remove(q.Tq, i)
//	}
//

// the current implementation circumvents the race condition,
// but it might miss out some tasks that should be deleted,
// and it's acceptable because those tasks will be timeout anyway
func (q *TimeQueue) DeleteTaskByIndex(i int, timeStamp, flag int64) {
	if i == -1 {
		return
	}
	q.Lock()
	defer q.Unlock()
	if q.Tq.Len() <= i {
		return
	}
	if (*q.Tq)[i].Event.Flag != flag || (*q.Tq)[i].Event.TimeStamp != timeStamp {
		return
	}
	heap.Remove(q.Tq, i)

}

func (q *TimeQueue) Tick() {
	task := q.Peek()
	if task != nil {
		//reset the next tick time of the timer
		timeNow := time.Now().UnixNano()
		duration := time.Duration(task.TimeStamp - timeNow)
		if duration < 0 {
			duration = 1
		}
		q.Timer.Reset(duration)
	}
}

func (q *TimeQueue) Len() int {
	q.RLock()
	defer q.RUnlock()
	return q.Tq.Len()
}

// Tick must bee called
func (q *TimeQueue) Ticking(notify chan Event) {
	for {
		<-q.Timer.C
	innerLoop:
		for {
			timeNow := time.Now().UnixNano()
			task := q.Peek()
			if task != nil {
				if task.TimeStamp > timeNow {
					duration := time.Duration(task.TimeStamp - timeNow)
					if duration > MAX_TICK_SECONDS {
						duration = MAX_TICK_SECONDS
					}
					q.Timer.Reset(duration)
					break innerLoop
				} else {
					notify <- q.PopTask().Event
					// fmt.Println(task)
				}
			} else {
				q.Timer.Reset(MAX_TICK_SECONDS)
				break innerLoop
			}

		}
	}
}
