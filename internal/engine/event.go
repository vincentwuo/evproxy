package engine

const (
	EV_TYPE_EPOLL       = 1
	EV_TYPE_TIMER_DELY  = 2
	EV_TYPE_TIMEOUT     = 3
	EV_TYPE_PROXY_CLOSE = 4
)

type Event struct {
	Ident     int // identifier of this event, usually file descriptor
	Ev        int // event mark
	Type      int // 1 epoll 2 timer dely 3 timeout 4 proxy_close
	TimeStamp int64
	Flag      int64 //might need some padding to improve the performance
}
