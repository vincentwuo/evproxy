package proxy

import (
	"sync/atomic"
	"syscall"
	"time"

	"github.com/vincentwuo/evproxy/internal/engine"
	"github.com/vincentwuo/evproxy/pkg/bytepool"
	"github.com/vincentwuo/evproxy/pkg/util"

	csmap "github.com/mhmtszr/concurrent-swiss-map"
	"golang.org/x/time/rate"
)

type Worker struct {
	ID          int
	poller      *engine.Poller
	notify      chan engine.Event
	conns       *csmap.CsMap[int, *Conn]
	Timer       *engine.TimeQueue
	bf          *bytepool.Pool
	bufferSize  int
	maxReadLoop int
	localBuffer []byte
}

func NewWorker(ID int, bufferSize int, maxReadLoop int) (*Worker, error) {
	poller, err := engine.OpenPoll(4096)
	if err != nil {
		return nil, err
	}

	w := Worker{
		ID:          ID,
		poller:      poller,
		conns:       csmap.Create[int, *Conn](),
		Timer:       engine.NewTimeQueue(),
		localBuffer: make([]byte, bufferSize, bufferSize),
		bufferSize:  bufferSize,
		bf:          bytepool.New(bufferSize),
		maxReadLoop: maxReadLoop,
		notify:      make(chan engine.Event, 4096*2),
	}
	return &w, nil
}

func NewWorkers(num int, bufferSize int, maxReadLoop int) []*Worker {

	var workers = make([]*Worker, num)

	for i := 0; i < num; i++ {
		workers[i], _ = NewWorker(i, bufferSize, maxReadLoop)
	}
	return workers
}

func (w *Worker) Run() {

	// go func() {
	// 	for {
	// 		count := w.conns.Count()
	// 		if count <= 30 {
	// 			str := ""
	// 			ct := 0
	// 			w.conns.Range(func(key int, value *Conn) (stop bool) {
	// 				ct++
	// 				str += fmt.Sprint(" fd:", key, "->", value.PeerConn.Fd, " isReady: ", value.ready, " isUpstream: ", value.Upstream, " isHup: ", value.isHup, " radadr", value.RemoteAddr, "/")
	// 				if ct > 10 {
	// 					return true
	// 				}
	// 				return false
	// 			})
	// 			fmt.Println("worker", w.ID, "conn num:", count, str)
	// 		} else {
	// 			fmt.Println("worker", w.ID, "conn num:", count)
	// 		}

	// 		time.Sleep(3 * time.Second)
	// 	}
	// }()
	go w.poller.Wait(w.notify)
	go w.Timer.Ticking(w.notify)

mainLoop:
	for {
		ev := <-w.notify
		c, ok := w.conns.Load(ev.Ident)
		if !ok {
			continue mainLoop
		}

		if c.proxy == nil || !c.proxy.enable || ev.Type == engine.PROXY_CLOSE_EVENT {
			if ev.Flag == c.flag {
				w.ClosePair(c)
			}
			continue
		}
		timeNow := time.Now()
		timeNowUnixNano := timeNow.UnixNano()

		if ev.Type == 1 {
			if ev.Ev&engine.ErrEvents != 0 {
				c.isHup = true
				if !c.Upstream && c.ready {
					//set a write timeout
					if c.proxy.writeTimeOut > 0 {
						timeout := timeNowUnixNano + c.proxy.writeTimeOut.Nanoseconds()
						timeOutTask := &engine.Task{
							TimeStamp: timeout,
							Event: engine.Event{
								Type:      engine.TIMEOUT_EVENT,
								TimeStamp: timeout,
								Ident:     c.Fd,
								Ev:        engine.ErrEvents,
								Flag:      c.flag,
							},
						}
						c.timeOutTask = timeOutTask
						w.Timer.PushTaskAndTick(timeOutTask)
					}

				}
				// fmt.Println("hup", ev.Ev&engine.InEventRaw, ev.Ev&engine.OutEventRaw)
				// if !c.Upstream && !c.ready {
				// 	//close all
				// 	// fmt.Println("source closed -> close all")
				// 	w.CloseCP(c)
				// 	continue
				// }
				// util.Println(c.Fd, "-", c.PeerConn.Fd, c.RemoteAddr, "meets hup", ev.Ev)

			}
		} else {
			if c.flag != ev.Flag {
				continue
			}
			if ev.Type == engine.TIMEOUT_EVENT {
				if !c.ready {
					//dial timeout
					w.ClosePair(c)

				} else if c.isHup {
					//write time out after the hup is true
					w.ClosePair(c)
				}
				continue
			}
		}

		if !c.ready {
			if !c.Upstream {
				// util.Println(c.Fd, "wait upstream to be connected")
				// wait upstream
				continue mainLoop

			} else {
				if !c.isHup {
					//upstream is connected successfully
					c.ready = true
					c.PeerConn.ready = true
					w.deleteTimeOutTask(c.PeerConn)

					//push a peerconn read event in the next loop
					w.Timer.PushTaskAndTick(&engine.Task{
						TimeStamp: timeNowUnixNano,
						Event: engine.Event{
							Ident:     c.PeerConn.Fd,
							Ev:        engine.InEventRaw,
							Type:      2,
							TimeStamp: timeNowUnixNano,
							Flag:      c.PeerConn.flag,
						},
					})
					if c.proxy.writeAfterDial != nil {
						c.Buffer = w.bf.Get()
						c.proxy.writeAfterDial(c)
						goto Write
					} else {
						continue mainLoop
					}

				} else {
					//close peer connections
					// util.Println("remote connection failed, close all")
					if c.proxy.loadBalancer != nil {
						c.proxy.loadBalancer.Unreachable(c.RemoteAddr)
					}
					w.ClosePair(c)
					continue

				}
			}

		}
		// ReadWriteLoop:
		if ev.Ev&engine.InEvents != 0 {
			w.handleReadEvent(ev, timeNow, c)
		}
	Write:
		if ev.Ev&engine.OutEvents != 0 {
			w.handleWriteEvent(ev, timeNow, c)
		}

	}

}

func (w *Worker) handleReadEvent(ev engine.Event, timeNow time.Time, c *Conn) {
	timeNowUnixNano := timeNow.UnixNano()
	if ev.TimeStamp >= c.LastTimeUnavilable {
		loopCt := 0
		var rm *rate.Limiter
		var limit float64
		if c.Upstream {
			rm = c.proxy.upRlimiter
			limit = c.proxy.upRLimit
		} else {
			rm = c.proxy.downRLimiter
			limit = c.proxy.downRLimit
		}
		if ev.Type == 1 {
			if ev.TimeStamp < c.nextTick {
				return
			}
			_ = limit
			// tokensCanRead := rm.Tokens()
			// if tokensCanRead <= 0.0 {
			// 	fmt.Println("token can")
			// 	//add
			// 	delayNano := int64((-tokensCanRead / limit) * 1e9)
			// 	//if the delay is bigger than a specific number,like 30?, the task should be dropped.
			// 	// util.Println("nano to second", time.Duration(delayNano).Seconds())

			// 	nextTick := timeNowUnixNano + delayNano
			// 	c.nextTick = nextTick
			// 	w.Timer.PushTaskAndTick(&engine.Task{
			// 		TimeStamp: nextTick,
			// 		Event: engine.Event{
			// 			TimeStamp: nextTick,
			// 			Ident:     ev.Ident,
			// 			Ev:        engine.InEventRaw,
			// 			Type:      2,
			// 			Flag:      c.flag,
			// 		},
			// 	})
			// 	goto Write

			// }
		}
		for l := 0; l < w.maxReadLoop; l++ {
			loopCt++
			//bug here? need ishup?
			if c.PeerConn.EndPos > 0 {
				if c.PeerConn.isHup {
					// util.Println(c.Fd, "close all from detecting peerconn is hup")
					w.ClosePair(c)
				}
				// w.tq.PushTaskAndTick(&engine.Task{
				// 	TimeStamp: timeNowUnixNano,
				// 	Event: engine.Event{
				// 		Ident: c.PeerConn.Fd,
				// 		Ev:    engine.OutEventRaw,
				// 		Type:  2,
				// 	},
				// })
				// tq.Tick()
				return
			}
			n, _ := syscall.Read(ev.Ident, w.localBuffer[0:])
			if n == -1 {
				// util.Println("read n=-1", err)
				if c.isHup {
					// util.Println(c.Fd, "read n=-1 hup is true")
					w.ClosePair(c)
				}
				c.LastTimeUnavilable = time.Now().UnixNano()
				//eagain?
				return
			} else if n == 0 {
				if c.isHup {
					//close all
					// util.Println(c.Fd, "close all from engine.read n=0,hup is true", err)
					w.ClosePair(c)
					return
				} else {
					// util.Println("read n =0 ", err, "hub what is this", c.Fd)
					return
				}
			}
			if c.Upstream {
				atomic.AddInt64(&c.proxy.trafficFromUpStream, int64(n))
			} else {
				atomic.AddInt64(&c.proxy.trafficFromDownStream, int64(n))
			}
			wn, _ := syscall.Write(c.PeerConn.Fd, w.localBuffer[:n])
			if wn == 0 {
				//peer is closed
				//close all
				if c.PeerConn.isHup {
					// util.Println("close all from write n=0 hup is true", err, c.PeerConn.RemoteAddr, "read n:", n)
					w.ClosePair(c)
				}
				util.Println("wn=0, brewak rwloop")
				return
			} else if wn == -1 {
				//eagain
				if c.PeerConn.Buffer == nil {

					buffer := w.bf.Get()
					cn := copy(*buffer, w.localBuffer[:n])
					c.PeerConn.Buffer = buffer
					c.PeerConn.CurPos = 0
					c.PeerConn.EndPos = cn
					util.Println("wn is -1,add to buff", cn)
				}
				// if c.PeerConn.isHup {
				// 	// util.Println("close all from write n=-1 hup is true")
				// 	// CloseAll(c, conns, epoll, bf)
				// 	break rwLoop
				// } else {
				// 	//put the data to it's buffer
				// 	if c.PeerConn.Buffer == nil {
				// 		buffer := bf.Get()
				// 		cn := copy(*buffer, localBuf[:n])
				// 		c.PeerConn.Buffer = buffer
				// 		c.PeerConn.CurPos = 0
				// 		c.PeerConn.EndPos = cn
				// 	} else {
				// 		panic("wtf")
				// 	}

				// }
			} else if wn < n {
				if c.PeerConn.Buffer == nil {
					buffer := w.bf.Get()
					cn := copy(*buffer, w.localBuffer[wn:n])
					c.PeerConn.Buffer = buffer
					c.PeerConn.CurPos = 0
					c.PeerConn.EndPos = cn
					// util.Println("wn is", wn, "add to buff", cn)
					w.Timer.PushTaskAndTick(&engine.Task{
						TimeStamp: timeNowUnixNano,
						Event: engine.Event{
							Ident:     c.PeerConn.Fd,
							Ev:        engine.OutEventRaw,
							Type:      2,
							TimeStamp: timeNowUnixNano,
							Flag:      c.PeerConn.flag,
						},
					})
					return

				}
			}

			resrv := rm.ReserveN(timeNow, n)
			if !resrv.OK() {
				//drop
				return
			}
			if resrv.Delay() > 0 {
				// util.Println("dely", resrv.Delay().Milliseconds())
				nextTick := timeNow.Add(resrv.Delay()).UnixNano()
				c.nextTick = nextTick
				w.Timer.PushTaskAndTick(&engine.Task{
					TimeStamp: nextTick,
					Event: engine.Event{
						Type:      2,
						Ident:     c.Fd,
						Ev:        engine.InEventRaw,
						TimeStamp: nextTick,
						Flag:      c.flag,
					},
				})
				return
			}
			if loopCt == w.maxReadLoop {
				//there still are data left
				//read in the next loop
				// nextTick := time.Now().UnixNano()
				nextTick := timeNowUnixNano + 5
				c.nextTick = nextTick
				w.Timer.PushTaskAndTick(&engine.Task{
					TimeStamp: nextTick,
					Event: engine.Event{
						Type:      2,
						Ident:     c.Fd,
						Ev:        engine.InEventRaw,
						TimeStamp: nextTick,
						Flag:      c.flag,
					},
				})
			}
		}

	}
}

func (w *Worker) handleWriteEvent(ev engine.Event, timeNow time.Time, c *Conn) {
	timeNowUnixNano := timeNow.UnixNano()
	if c.Buffer != nil && c.EndPos > 0 {
		wn, _ := syscall.Write(c.Fd, (*c.Buffer)[c.CurPos:c.EndPos])
		// util.Println("buffer is full, so try writing out", wn)
		// if wn >= 0 {
		// 	// *wcount += wn
		// 	// util.Println("writable wcount:", wcount)
		// }
		// util.Println("write n:", wn, "total", wcount)
		if wn < c.EndPos-c.CurPos {
			if wn == 0 {
				// util.Println("close all from write, wn = 0 hup", c.isHup)
				w.ClosePair(c)

			} else {
				//-1 or less than curpos
				//need to be written again
				if wn != -1 {
					c.CurPos += wn
				}
				//what is the appriciate delay?
				//1e3 = 1 microsecond
				// util.Println("parttial write")
				w.Timer.PushTaskAndTick(&engine.Task{
					TimeStamp: timeNowUnixNano + 1e3,
					Event: engine.Event{
						Ident:     c.Fd,
						Ev:        engine.OutEventRaw,
						Type:      2,
						TimeStamp: timeNowUnixNano + 1e3,
						Flag:      c.flag,
					},
				})
			}
		} else {
			c.CurPos = 0
			c.EndPos = 0
			w.bf.Put(c.Buffer)
			c.Buffer = nil
			// wake peer to read
			tn := time.Now().UnixNano()
			c.PeerConn.nextTick = tn
			w.Timer.PushTaskAndTick(&engine.Task{
				TimeStamp: tn,
				Event: engine.Event{
					Ident:     c.PeerConn.Fd,
					Ev:        engine.InEventRaw,
					Type:      2,
					TimeStamp: tn,
					Flag:      c.PeerConn.flag,
				},
			})
		}

	}
}

func (w *Worker) deleteTimeOutTask(c *Conn) {
	if c.timeOutTask != nil {
		w.Timer.DeleteTaskByIndex(c.timeOutTask.Index, c.timeOutTask.TimeStamp, c.flag)
		c.timeOutTask = nil
	}
}

func (w *Worker) putBuffer(c *Conn) {
	if c.Buffer != nil {
		w.bf.Put(c.Buffer)
		c.Buffer = nil
	}
}

// ClosePair will close the c and it's peer
func (w *Worker) ClosePair(c *Conn) {
	w.poller.UnWatch(c.Fd)
	w.poller.UnWatch(c.PeerConn.Fd)
	w.putBuffer(c)
	w.deleteTimeOutTask(c)

	w.putBuffer(c.PeerConn)
	w.deleteTimeOutTask(c.PeerConn)

	w.conns.Delete(c.Fd)
	w.conns.Delete(c.PeerConn.Fd)

	syscall.Close(c.Fd)
	syscall.Close(c.PeerConn.Fd)

	if !c.Upstream {
		// syscall.Close(c.rawFd)
		c.proxy.connFDs[w.ID].Delete(c.Fd)
	} else {
		// syscall.Close(c.PeerConn.rawFd)
		c.proxy.connFDs[w.ID].Delete(c.PeerConn.Fd)
	}

	c.proxy.conLimiter.Release()
}
