package proxy

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/vincentwuo/evproxy/internal/engine"
	"github.com/vincentwuo/evproxy/pkg/concurrent"
	"github.com/vincentwuo/evproxy/pkg/lb"
	"github.com/vincentwuo/evproxy/pkg/util"

	"github.com/libp2p/go-reuseport"
	csmap "github.com/mhmtszr/concurrent-swiss-map"
	"golang.org/x/time/rate"
)

type PorxyType uint8

const (
	TCP PorxyType = 1
	UDP PorxyType = 2
)

type dialRequest struct {
	fd int
	sa syscall.Sockaddr
}

type Proxy struct {
	Type     PorxyType
	acceptor *engine.Poller
	enable   bool
	laddrIP  net.IP
	laddr    string
	remotes  []string
	// Listener           net.Listener
	listenerFile *os.File //important, withouit os.File the file descriptor may be GC collected
	downRLimit   float64
	upRLimit     float64
	downRLimiter *rate.Limiter
	upRlimiter   *rate.Limiter

	trafficFromDownStream int64
	trafficFromUpStream   int64

	LastTimeUnavilable int64
	connFDs            []*csmap.CsMap[int, int]
	workers            []*Worker
	conLimiter         *concurrent.AtomicLimiter
	loadBalancer       lb.LoadBalancer
	dialWorkerNum      int
	dialTimeOut        time.Duration
	writeTimeOut       time.Duration
	writeAfterDial     func(c *Conn)
	dialRequestCh      chan dialRequest
	count              int64
	stopCh             chan struct{}
}

type ProxyOption func(*Proxy)

func WithConcurrentLimit(limit int64) ProxyOption {
	return func(p *Proxy) {
		p.conLimiter.Reset(limit)
	}
}

func WithGlobalConcurrentLimiter(limiter *concurrent.AtomicLimiter) ProxyOption {
	return func(p *Proxy) {
		p.conLimiter = limiter
	}
}

func WithBandwidthLimit(readFromSourceLimit, readFromRemoteLimit float64) ProxyOption {
	return func(p *Proxy) {
		p.downRLimit = readFromSourceLimit
		p.downRLimiter = rate.NewLimiter(rate.Limit(readFromSourceLimit), int(readFromSourceLimit))

		p.upRLimit = readFromRemoteLimit
		p.upRlimiter = rate.NewLimiter(rate.Limit(readFromRemoteLimit), int(readFromRemoteLimit))
	}
}
func WithGlobalBandwidthLimit(readFromSourceLimit, readFromRemoteLimit float64, readFromSourceLimiter, readFromRemoteLimiter *rate.Limiter) ProxyOption {
	return func(p *Proxy) {
		p.downRLimit = readFromSourceLimit
		p.downRLimiter = readFromRemoteLimiter
		p.downRLimiter.Limit()
		p.upRLimit = readFromRemoteLimit
		p.upRlimiter = readFromRemoteLimiter
	}
}

func WithDialworkerNum(num int) ProxyOption {
	return func(p *Proxy) {
		p.dialWorkerNum = num
	}
}

func WithDialTimeout(timeout time.Duration) ProxyOption {
	return func(p *Proxy) {
		p.dialTimeOut = timeout
	}
}

func WithWriteTimeout(timeout time.Duration) ProxyOption {
	return func(p *Proxy) {
		p.writeTimeOut = timeout
	}
}

func WithWriteFuncAfterDial(f func(*Conn)) ProxyOption {
	return func(p *Proxy) {
		p.writeAfterDial = f
	}
}

func NewTCPProxy(workers []*Worker, laddr string, raddrs []string, unreachableThreshold int, thresholdInterval, checkInterval time.Duration, opts ...ProxyOption) (*Proxy, error) {
	const (
		defaultConcurrentLimit             = 0
		defaultDialWorkerNum               = 16
		defaultReadFromSourceLimit float64 = 1e12
		defaultReadFromRemoteLimit float64 = 1e12
		defaultDialTimeOut                 = 30
		defaultWriteTimeOut                = 30
	)

	workerCount := len(workers)
	if workerCount == 0 {
		return nil, errors.New("no worker found")
	}

	tcpAddr, err := net.ResolveTCPAddr("tcp", laddr)
	if err != nil {
		return nil, err
	}
	var lber lb.LoadBalancer

	lber, err = lb.NewIPhash("tcp", raddrs, unreachableThreshold, thresholdInterval, checkInterval)
	if err != nil {
		return nil, err
	}
	cl, _ := concurrent.NewAtomicLimiter(defaultConcurrentLimit)
	acceptor, _ := engine.OpenPoll(16)

	p := Proxy{
		Type:           TCP,
		acceptor:       acceptor,
		enable:         true,
		laddrIP:        tcpAddr.IP,
		laddr:          laddr,
		remotes:        raddrs,
		downRLimit:     defaultReadFromSourceLimit,
		upRLimit:       defaultReadFromRemoteLimit,
		downRLimiter:   rate.NewLimiter(rate.Limit(defaultReadFromSourceLimit), int(defaultReadFromSourceLimit)),
		upRlimiter:     rate.NewLimiter(rate.Limit(defaultReadFromRemoteLimit), int(defaultReadFromRemoteLimit)),
		connFDs:        make([]*csmap.CsMap[int, int], workerCount),
		workers:        make([]*Worker, workerCount),
		conLimiter:     cl,
		loadBalancer:   lber,
		dialWorkerNum:  defaultDialWorkerNum,
		dialRequestCh:  make(chan dialRequest, 4096),
		dialTimeOut:    defaultDialTimeOut * time.Second,
		writeTimeOut:   defaultWriteTimeOut * time.Second,
		writeAfterDial: nil,
		stopCh:         make(chan struct{}),
	}
	for i := 0; i < workerCount; i++ {
		p.connFDs[i] = csmap.Create[int, int]()
		p.workers[i] = workers[i]
	}

	for _, opt := range opts {
		opt(&p)
	}

	ln, err := reuseport.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}
	// p.Listener = ln
	tcpln := ln.(*net.TCPListener)
	f, err := tcpln.File()
	if err != nil {
		return nil, err
	}
	p.listenerFile = f
	syscall.SetNonblock(int(f.Fd()), true)
	p.acceptor.Watch(int(f.Fd()))
	//make sure
	syscall.SetNonblock(int(f.Fd()), true)
	return &p, nil
}

// Accepting looping
func (p *Proxy) Accepting() {
	switch p.Type {
	case TCP:
		p.tcpAccepting()
	case UDP:
	default:

	}
}

func (p *Proxy) tcpAccepting() {

	var notify = make(chan engine.Event, 16)
	go p.acceptor.Wait(notify)
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < p.dialWorkerNum; i++ {
		go p.dialWorkLoop(ctx)
	}
acceptLoop:
	for {
		select {
		case ev := <-notify:
			for {
				if p.enable {
					//accept
					fd, sa, err := syscall.Accept(ev.Ident)
					if err != nil {
						if errors.Is(err, syscall.EAGAIN) && fd == -1 {
							//have accepted all the requests in the RECV-Q
							continue acceptLoop
						}
						//other error
						// util.Println("accpet", ev.Ident, err)
						continue acceptLoop
					}
					if !p.enable {
						syscall.Close(fd)
					} else {
						syscall.SetNonblock(fd, true)
						p.dialRequestCh <- dialRequest{
							fd: fd,
							sa: sa,
						}
					}

				} else {
					continue acceptLoop
				}
			}
		case <-p.stopCh:
			cancel()
			return
		}

	}

}

func (p *Proxy) dialWorkLoop(ctx context.Context) {
	workerCount := len(p.workers)
	rSource := rand.NewSource(time.Now().UnixNano())
	generator := rand.New(rSource)
	for {
		select {
		case request := <-p.dialRequestCh:
			fd := request.fd
			sa := request.sa

			canAccept, _ := p.conLimiter.Acquire()
			if !canAccept {
				syscall.Close(fd)
				continue
			}
			downStreamRemoteAddr := util.SockaddrToTCPOrUnixAddr(sa)

			//get remote addr
			remoteAddr, err := p.loadBalancer.Get(downStreamRemoteAddr.String())
			if err != nil {
				util.Println("load balance get", err)
				p.closeFdwithConnLimiterRelease(fd)
				continue
			}
			timeNowNano := time.Now().UnixNano()

			//send to worker
			//assigin the connection peer to a worker
			//use round robin
			workerID := int(atomic.LoadInt64(&p.count)) % workerCount
			worker := p.workers[workerID]

			// worker.conns.Store(rfd, targetConn)
			// dial to the remote
			rfd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_IP)
			if err != nil {
				util.Println("bind", err)
				p.closeFdwithConnLimiterRelease(fd)
				continue
			}
			syscall.SetNonblock(rfd, true)
			// syscall.SetsockoptString(rfd, syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifname)
			dialSa, _ := util.IpToSockaddr(syscall.AF_INET, p.laddrIP, 0, "")
			err = syscall.Bind(rfd, dialSa)
			if err != nil {
				util.Println("bind", err)
				p.closeFdwithConnLimiterRelease(fd, rfd)
				continue
			}

			rsa, err := util.IpToSockaddr(syscall.AF_INET, remoteAddr.IP, remoteAddr.Port, "")
			if err != nil {
				util.Println("rsa parse", err)
				p.closeFdwithConnLimiterRelease(fd, rfd)
				continue
			}

			err = syscall.Connect(rfd, rsa)
			if err != nil {
				if !errors.Is(err, syscall.EINPROGRESS) {
					util.Println("connect to the remote:", err)
					p.closeFdwithConnLimiterRelease(fd, rfd)
					continue
				}

			}

			sourceConn := &Conn{
				Fd:         fd,
				RemoteAddr: downStreamRemoteAddr.String(),
				proxy:      p,
				flag:       timeNowNano,
			}

			targetConn := &Conn{
				Fd:         rfd,
				RemoteAddr: remoteAddr.AddrStr,
				Upstream:   true,
				proxy:      p,
				// listenerFd: listenerFD,
				flag: timeNowNano,
			}
			sourceConn.PeerConn = targetConn
			targetConn.PeerConn = sourceConn
			//add the dial deadline
			//add some random deviation to amortize some pressure
			//1e6 = 1 mili second
			deviation := 1 + generator.Int63n(1000)*1e6
			timeStamp := p.dialTimeOut.Nanoseconds() + deviation + timeNowNano
			timeOutTask := &engine.Task{
				TimeStamp: timeStamp,
				Event: engine.Event{
					Type:      3,
					TimeStamp: timeStamp,
					Ident:     fd,
					Ev:        engine.ErrEvents,
					Flag:      timeNowNano,
				},
			}
			sourceConn.timeOutTask = timeOutTask

			worker.conns.Store(fd, sourceConn)
			worker.conns.Store(rfd, targetConn)

			p.connFDs[workerID].Store(fd, rfd)

			worker.poller.Watch(fd)
			worker.poller.Watch(rfd)

			worker.Timer.PushTaskAndTick(timeOutTask)

			atomic.AddInt64(&p.count, 1)
		case <-ctx.Done():
			return

		}

	}
}

// blocking and time-cosuming function
// takes at least 1 seconds
// WARNING: might have some bugs due to the race condition
func (p *Proxy) Close() int {
	p.enable = false
	p.listenerFile.Close()
	p.acceptor.UnWatch(int(p.listenerFile.Fd()))
	p.acceptor.Close()
	p.loadBalancer.Stop()
	p.stopCh <- struct{}{}
	// p.Listener.Close()
	//wait variable sync
	// time.Sleep(1 * time.Second)

	for ct := 0; ct < 2; ct++ {
		for i := range p.connFDs {
			p.connFDs[i].Range(func(downFd int, UpFd int) bool {

				p.workers[i].poller.UnWatch(downFd)
				p.workers[i].poller.UnWatch(UpFd)
				p.workers[i].conns.Delete(downFd)
				p.workers[i].conns.Delete(UpFd)

				syscall.Close(downFd)
				syscall.Close(UpFd)

				p.conLimiter.Release()
				return false
			})
			p.connFDs[i].Clear()
		}
		if ct == 0 {
			time.Sleep(1 * time.Second)
		}
	}
	var left = 0
	for i := range p.connFDs {
		left += p.connFDs[i].Count()
	}
	return left

}

func (p *Proxy) closeFdwithConnLimiterRelease(fds ...int) {
	for fd := range fds {
		syscall.Close(fd)
	}
	p.conLimiter.Release()
}

func (p *Proxy) TrafficCount() (up, down int64) {
	return p.trafficFromUpStream, p.trafficFromDownStream
}

func (p *Proxy) StreamCount() int64 {
	return p.conLimiter.Count()
}

func (p *Proxy) SetBandLimitNumber(readFromDownStream, readFromUpStream float64) {
	p.downRLimit = readFromDownStream
	p.upRLimit = readFromUpStream
}

func (p *Proxy) Remotes() []string {
	return p.remotes
}
