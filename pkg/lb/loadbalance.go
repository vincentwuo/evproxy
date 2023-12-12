package lb

import "net"

type RemoteAddr struct {
	AddrStr      string
	IP           net.IP
	Port         int
	recordTime   int64
	unreachCount int
	status       bool
}

type LoadBalancer interface {
	Get(signature string) (RemoteAddr, error) //return addr string and error
	Unreachable(addr string)
	Stop()
}
