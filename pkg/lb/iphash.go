package lb

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/cespare/xxhash"
)

const (
	DEFAULT_THRESHOLD_INTERVAL = 30 * time.Second
)

var (
	ERROR_NO_REACHABLE_IP = errors.New("no reachable ip found.")
)

type IPhash struct {
	sync.RWMutex
	network              string
	length               uint64
	addrs                []RemoteAddr
	checkInterval        time.Duration
	unreachableThreshold int
	thresholdInterval    time.Duration
	stop                 chan struct{}
}

func NewIPhash(network string, addrs []string, unreachableThreshold int, thresholdInterval time.Duration, checkInterval time.Duration) (*IPhash, error) {
	length := len(addrs)
	if length == 0 {
		return nil, errors.New("addrs is empty")
	}
	if unreachableThreshold <= 0 {
		unreachableThreshold = 1
	}
	if checkInterval < 0 {
		checkInterval = 1 * time.Second
	}

	if thresholdInterval <= 0 {
		thresholdInterval = DEFAULT_THRESHOLD_INTERVAL
	}

	iHash := IPhash{
		network:              network,
		checkInterval:        checkInterval,
		length:               uint64(length),
		unreachableThreshold: unreachableThreshold,
		thresholdInterval:    thresholdInterval,
		stop:                 make(chan struct{}),
	}
	for i := 0; i < length; i++ {
		if addrs[i] == "" {
			return nil, errors.New("upstream addr got an empty one")
		}
		addr, err := net.ResolveTCPAddr("tcp", addrs[i])
		if err != nil {
			return nil, err
		}
		iHash.addrs = append(iHash.addrs, RemoteAddr{
			AddrStr: addrs[i],
			IP:      addr.IP,
			Port:    addr.Port,
			status:  true,
		})
	}

	go iHash.check()
	return &iHash, nil

}

func (iph *IPhash) Get(signature string) (RemoteAddr, error) {
	if iph.length == 1 {
		return iph.addrs[0], nil
	}
	//## need consistent hash?
	//performance issue?
	hashN := int(xxhash.Sum64String(signature) % iph.length)
	iph.RLock()
	defer iph.RUnlock()
	if iph.addrs[hashN].status == true {
		return iph.addrs[hashN], nil
	} else {
		//pick next
		found := false
		var retAddr RemoteAddr
		for i := hashN + 1; i < int(iph.length); i++ {
			if iph.addrs[i].status == true {
				found = true
				retAddr = iph.addrs[i]
				break
			}
		}
		if !found {
			//not found any valid addr in the array after the indice hashN
			//search for the first half
			for i := 0; i < hashN; i++ {
				if iph.addrs[i].status == true {
					found = true
					retAddr = iph.addrs[i]
					break
				}
			}
		}
		if found {
			return retAddr, nil
		} else {
			return RemoteAddr{}, ERROR_NO_REACHABLE_IP
		}
	}
}

func (iph *IPhash) Unreachable(addr string) {
	if iph.length == 1 {
		return
	}
	iph.Lock()
	defer iph.Unlock()
	timeNowUnixNano := time.Now().UnixNano()
	for id, addrRepresention := range iph.addrs {
		if addrRepresention.AddrStr == addr {
			if iph.addrs[id].recordTime == 0 {
				iph.addrs[id].recordTime = timeNowUnixNano
				iph.addrs[id].unreachCount++
			} else {
				if timeNowUnixNano-iph.addrs[id].recordTime <= iph.thresholdInterval.Nanoseconds() {
					iph.addrs[id].unreachCount++
					if iph.addrs[id].unreachCount >= iph.unreachableThreshold {
						iph.addrs[id].status = false
					}
				} else {
					iph.addrs[id].recordTime = timeNowUnixNano
					iph.addrs[id].unreachCount = 1

				}
			}
			// fmt.Println(iph.addrs, iph.addrs[id].unreachCount)
			if iph.addrs[id].unreachCount >= iph.unreachableThreshold {
				iph.addrs[id].status = false
				// fmt.Println(iph.addrs, iph.addrs[id].unreachCount, iph.addrs[id].status)
			}

			return
		}
	}
}

type addrForCheck struct {
	index int
	addr  string
}

func (iph *IPhash) check() {
	tick := time.NewTicker(iph.checkInterval)
	defer tick.Stop()
	for {
		select {
		case <-iph.stop:
			return
		case <-tick.C:
			var waitForChecks []addrForCheck
			iph.RLock()
			for i := 0; i < len(iph.addrs); i++ {
				if iph.addrs[i].status == false {
					waitForChecks = append(waitForChecks, addrForCheck{
						index: i,
						addr:  iph.addrs[i].AddrStr,
					})
				}
			}
			iph.RUnlock()

			//check
			if iph.network == "tcp" {
				for _, check := range waitForChecks {
					// fmt.Println("load balance checking:", check.addr)
					conn, err := net.Dial("tcp", check.addr)
					if err == nil {
						conn.Close()
						iph.Lock()
						iph.addrs[check.index].status = true
						iph.addrs[check.index].unreachCount = 0
						iph.Unlock()
					}
				}
			}

		}
	}
}

func (iph *IPhash) Stop() {
	iph.stop <- struct{}{}
}
