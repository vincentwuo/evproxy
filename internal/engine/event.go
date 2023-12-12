package engine

const (
	EPOLL_EVENT       = 1
	DELY_EVENT        = 2
	TIMEOUT_EVENT     = 3
	PROXY_CLOSE_EVENT = 4
)

type Event struct {
	Ident     int // identifier of this event, usually file descriptor
	Ev        int // event mark
	Type      int // 1 epoll 2 timer 3 timeout
	TimeStamp int64
	Flag      int64 //might need some padding to improve the performance
}
