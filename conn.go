package proxy

import (
	"github.com/vincentwuo/evproxy/internal/engine"
)

type Conn struct {
	Fd                 int
	PeerConn           *Conn
	LocalAddr          string
	RemoteAddr         string
	Upstream           bool
	Buffer             *[]byte
	LastTimeUnavilable int64 // ?need?
	proxy              *Proxy
	CurPos             int
	EndPos             int
	ready              bool
	isHup              bool
	timeOutTask        *engine.Task
	nextTick           int64
	// listenerFd         int   //alleviate GC pressure?
	flag int64 //store the creation time. used as a distinguishing mark from other connections that use the same fd.
}

// func (c *Conn) close() error {
// 	return syscall.Close(c.Fd)
// }

// // closeSP close self and the peer conn
// func (c *Conn) closeSP() (selfError error, peerError error) {
// 	selfError = c.close()

// 	if c.PeerConn != nil {
// 		peerError = c.PeerConn.close()
// 	}
// 	return
// }
