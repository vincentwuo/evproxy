//go:build linux

// edit from https://github.com/xtaci/gaio/blob/master/aio_generic.go
package engine

import (
	"errors"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// ErrEvents represents exceptional events that are not read/write, like socket being closed,
	// reading/writing from/to a closed socket, etc.
	ErrEvents = unix.EPOLLERR | unix.EPOLLHUP | unix.EPOLLRDHUP
	// OutEvents combines EPOLLOUT event and some exceptional events.
	OutEvents = ErrEvents | unix.EPOLLOUT
	// InEvents combines EPOLLIN/EPOLLPRI events and some exceptional events.
	InEvents = ErrEvents | unix.EPOLLIN | unix.EPOLLPRI

	InEventRaw  = unix.EPOLLIN | unix.EPOLLPRI
	OutEventRaw = unix.EPOLLOUT
)

const (
	RDHUB = syscall.EPOLLRDHUP
	RSET  = syscall.EPOLLIN
	WSET  = syscall.EPOLLOUT
)

var (
	// ErrUnsupported means the watcher cannot support this type of connection
	ErrUnsupported = errors.New("unsupported connection type")
	// ErrNoRawConn means the connection has not implemented SyscallConn
	ErrNoRawConn = errors.New("net.Conn does implement net.RawConn")
	// ErrWatcherClosed means the watcher is closed
	ErrWatcherClosed = errors.New("watcher closed")
	// ErrPollerClosed suggest that poller has closed
	ErrPollerClosed = errors.New("poller closed")
	// ErrConnClosed means the user called Free() on related connection
	ErrConnClosed = errors.New("connection closed")
	// ErrDeadline means the specific operation has exceeded deadline before completion
	ErrDeadline = errors.New("operation exceeded deadline")
	// ErrEmptyBuffer means the buffer is nil
	ErrEmptyBuffer = errors.New("empty buffer")
	// ErrCPUID indicates the given cpuid is invalid
	ErrCPUID = errors.New("no such core")
)

// _EPOLLET value is incorrect in syscall
const (
	_EPOLLET      = 0x80000000
	_EFD_NONBLOCK = 0x800
)

var (
	zeroTime = time.Time{}
)

// OpType defines Operation Type
type OpType int

const (
	// OpRead means the aiocb is a read operation
	OpRead OpType = iota
	// OpWrite means the aiocb is a write operation
	OpWrite
	// internal operation to delete an related resource
	opDelete
)

const (
	EV_HUB   = unix.EPOLLERR | unix.EPOLLHUP | unix.EPOLLRDHUP
	EV_READ  = ErrEvents | unix.EPOLLIN | unix.EPOLLPRI
	EV_WRITE = ErrEvents | unix.EPOLLOUT
)

type Poller struct {
	cpuid int32
	// poolGeneric
	mu     sync.Mutex // mutex to protect fd closing
	pfd    int        // epoll fd
	efd    int        // eventfd
	efdbuf []byte

	maxEvents int

	// closing signal
	die     chan struct{}
	dieOnce sync.Once
}

func ConnFD(conn net.Conn) (nfd int, err error) {
	sc, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return -1, ErrUnsupported
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return -1, ErrUnsupported
	}

	// Control() guarantees the integrity of file descriptor
	ec := rc.Control(func(fd uintptr) {
		nfd = int(fd)
	})

	if ec != nil {
		return -1, ec
	}

	return
}

func ShutUpConn(conn net.Conn) error {
	sc, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return ErrUnsupported
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return ErrUnsupported
	}
	ec := rc.Control(func(fd uintptr) {
		syscall.Close(int(fd))
	})

	if ec != nil {
		return ec
	}
	return nil
}

// func DupCloseOnExecOld(fd int) (int, string, error) {
// 	syscall.ForkLock.RLock()
// 	defer syscall.ForkLock.RUnlock()
// 	newFD, err := syscall.Dup(fd)
// 	if err != nil {
// 		return -1, "dup", err
// 	}
// 	syscall.CloseOnExec(newFD)
// 	return newFD, "", nil
// }

// dupconn use RawConn to dup() file descriptor
func Dupconn(conn net.Conn) (newfd, oldfd int, err error) {
	sc, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return -1, -1, ErrUnsupported
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return -1, -1, ErrUnsupported
	}

	// Control() guarantees the integrity of file descriptor
	ec := rc.Control(func(fd uintptr) {
		oldfd = int(fd)
		newfd, err = syscall.Dup(int(fd))
	})

	if ec != nil {
		return -1, -1, ec
	}
	syscall.CloseOnExec(newfd)
	return
}

func OpenPoll(maxEvents int) (*Poller, error) {
	fd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	r0, _, e0 := syscall.Syscall(syscall.SYS_EVENTFD2, 0, _EFD_NONBLOCK, 0)
	if e0 != 0 {
		syscall.Close(fd)
		return nil, err
	}

	if err := syscall.EpollCtl(fd, syscall.EPOLL_CTL_ADD, int(r0),
		&syscall.EpollEvent{Fd: int32(r0),
			Events: syscall.EPOLLIN | _EPOLLET,
		},
	); err != nil {
		syscall.Close(fd)
		syscall.Close(int(r0))
		return nil, err
	}

	p := new(Poller)
	p.pfd = fd
	p.efd = int(r0)
	p.efdbuf = make([]byte, 8)
	p.die = make(chan struct{})
	p.cpuid = -1
	p.maxEvents = maxEvents

	return p, err
}

// Close the poller
func (p *Poller) Close() error {
	p.dieOnce.Do(func() {
		close(p.die)
	})
	return p.wakeup()
}

func (p *Poller) Watch(fd int) (err error) {
	p.mu.Lock()
	//notice: deleted the parameter syscall.EPOLLONESHOT |
	err = syscall.EpollCtl(p.pfd, syscall.EPOLL_CTL_ADD, int(fd), &syscall.EpollEvent{Fd: int32(fd), Events: syscall.EPOLLRDHUP | syscall.EPOLLIN | syscall.EPOLLOUT | _EPOLLET})
	p.mu.Unlock()
	return
}

func (p *Poller) UnWatch(fd int) (err error) {
	p.mu.Lock()
	//notice: deleted the parameter syscall.EPOLLONESHOT |
	err = syscall.EpollCtl(p.pfd, syscall.EPOLL_CTL_DEL, int(fd), &syscall.EpollEvent{Fd: int32(fd)})
	p.mu.Unlock()
	return
}

func (p *Poller) Rearm(fd int, read bool, write bool) (err error) {
	p.mu.Lock()
	var flag uint32
	flag = syscall.EPOLLONESHOT | _EPOLLET
	if read {
		flag |= syscall.EPOLLIN | syscall.EPOLLRDHUP
	}
	if write {
		flag |= syscall.EPOLLOUT
	}

	err = syscall.EpollCtl(p.pfd, syscall.EPOLL_CTL_MOD, int(fd), &syscall.EpollEvent{Fd: int32(fd), Events: flag})
	p.mu.Unlock()
	return
}

// wakeup interrupt epoll_wait
func (p *Poller) wakeup() error {
	p.mu.Lock()
	if p.efd != -1 {
		var x uint64 = 1
		// eventfd has set with EFD_NONBLOCK
		_, err := syscall.Write(p.efd, (*(*[8]byte)(unsafe.Pointer(&x)))[:])
		p.mu.Unlock()
		return err
	}
	p.mu.Unlock()
	return ErrPollerClosed
}

func (p *Poller) CloseBlockWait() {
	p.mu.Lock()
	syscall.Close(p.pfd)
	syscall.Close(p.efd)
	p.pfd = -1
	p.efd = -1
	p.mu.Unlock()
}

// !!!need a way to alleviate the flood of 64 byte packet
func (p *Poller) Wait(chEventNotify chan Event) {
	// p.initCache(cap(chEventNotify) + 2)
	events := make([]syscall.EpollEvent, p.maxEvents)
	// close poller fd & eventfd in defer
	defer func() {
		p.mu.Lock()
		syscall.Close(p.pfd)
		syscall.Close(p.efd)
		p.pfd = -1
		p.efd = -1
		p.mu.Unlock()
	}()
	// epoll eventloop
	for {
		select {
		case <-p.die:
			return
		default:
			n, err := syscall.EpollWait(p.pfd, events, -1)
			if err == syscall.EINTR {
				continue
			}
			if err != nil {
				return
			}

			// // load from cache
			// pe := p.loadCache(n)
			// event processing
			timeNowNano := time.Now().UnixNano()
			for i := 0; i < n; i++ {
				ev := &events[i]
				if int(ev.Fd) == p.efd {
					syscall.Read(p.efd, p.efdbuf) // simply consume
					// check cpuid
					// if cpuid := atomic.LoadInt32(&p.cpuid); cpuid != -1 {
					// 	fmt.Println(cpuid)
					// 	setAffinity(cpuid)
					// 	atomic.StoreInt32(&p.cpuid, -1)
					// }
				} else {
					e := Event{Ident: int(ev.Fd), Type: 1, TimeStamp: timeNowNano, Ev: int(ev.Events)}

					select {
					case chEventNotify <- e:
					case <-p.die:
						return
					}
				}
			}

		}
	}
}

// Errno values.
var (
	errEAGAIN error = syscall.EAGAIN
	errEINVAL error = syscall.EINVAL
	errENOENT error = syscall.ENOENT
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case syscall.EAGAIN:
		return errEAGAIN
	case syscall.EINVAL:
		return errEINVAL
	case syscall.ENOENT:
		return errENOENT
	}
	return e
}

var _zero uintptr

// raw read for nonblocking op to avert context switch
func RawRead(fd int, p []byte) (n int, err error) {
	var _p0 unsafe.Pointer
	if len(p) > 0 {
		_p0 = unsafe.Pointer(&p[0])
	} else {
		_p0 = unsafe.Pointer(&_zero)
	}
	r0, _, e1 := syscall.RawSyscall(syscall.SYS_READ, uintptr(fd), uintptr(_p0), uintptr(len(p)))
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}

// raw write for nonblocking op to avert context switch
func RawWrite(fd int, p []byte) (n int, err error) {
	var _p0 unsafe.Pointer
	if len(p) > 0 {
		_p0 = unsafe.Pointer(&p[0])
	} else {
		_p0 = unsafe.Pointer(&_zero)
	}
	r0, _, e1 := syscall.RawSyscall(syscall.SYS_WRITE, uintptr(fd), uintptr(_p0), uintptr(len(p)))
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}
