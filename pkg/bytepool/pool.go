package bytepool

import (
	"sync"
)

type Pool struct {
	p     *sync.Pool
	size  int
	Gtime int64
	Ptime int64
}

func New(bufSize int) *Pool {
	return &Pool{
		p: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, bufSize, bufSize)
				return &buf
			},
		},
		size: bufSize,
	}
}

func (bp *Pool) Get() *[]byte {
	// t := atomic.AddInt64(&bp.Gtime, 1)
	// fmt.Println("get time:", t)
	// return bp.p.Get().([]byte)[:bp.size]
	b := bp.p.Get().(*[]byte)
	c := (*b)[:bp.size]
	return &c
	// b = &((*b)[:bp.size])
	// return bp.p.Get().(*[]byte)
}

// 单个对象多次重复put导致bug
func (bp *Pool) Put(buf *[]byte) {
	// t := atomic.AddInt64(&bp.Ptime, 1)
	// fmt.Println("put time:", t)
	bp.p.Put(buf)

}
