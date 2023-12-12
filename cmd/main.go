package main

import (
	"flag"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	proxy "github.com/vincentwuo/evproxy"
	"github.com/vincentwuo/evproxy/pkg/util"
)

const (
	BUFFER_SIZE        = 1024 * 32
	MAX_READLOOP_COUNT = 8
)

var (
	wokerNum              = flag.Int("n", 0, "the number of workers to handlle the data transfer. default 0 will set it to the number of CPU cores")
	bindAddr              = flag.String("bind", "", "addr to accept downstream data. Example: 0.0.0.0:8890 ")
	upstreamAddrs         = flag.String("upstreams", "", "upstream addrs define where the data will be transfer to , need to be splited by comma. Example: 1.1.1.1:123, 2.2.2.2:123")
	upstreamCheckInterval = flag.Int("check", 30, "(unit:second) the interval to check whether the unreachable upstream endpoint is back online or not")
	concurrentLimit       = flag.Int64("c", 0, "cocurrent limit. '0' means no limit")
	readSpeed             = flag.Int("rmbps", 0, "mbyte per second for reading from the downstream. '0.0' means no limit")
	writeSpeed            = flag.Int("wmbps", 0, "mbyte per second for reading from the upstream. '0.0' means no limit")
	dialTimeout           = flag.Int("dtimeout", 15, "(unit:second) the timeout when dialing to the upstream")
	writeTimeout          = flag.Int("wtimeout", 30, "(unit:second) the timeout for the write operation when one of the peer is closed.")
)

func main() {
	flag.Parse()
	ctrlc := make(chan os.Signal)
	signal.Notify(ctrlc, os.Interrupt)

	if *wokerNum == 0 {
		*wokerNum = runtime.NumCPU()
	}
	if *bindAddr == "" {
		util.Logger().Fatal("bind addr is empty")
	}
	splitedUpstreamAddr := strings.Split(*upstreamAddrs, ",")
	for i := 0; i < len(splitedUpstreamAddr); i++ {
		splitedUpstreamAddr[i] = strings.TrimSpace(splitedUpstreamAddr[i])
	}
	if *readSpeed <= 0 {
		*readSpeed = 1e13
	} else {
		*readSpeed = *readSpeed * 1024 * 1024
	}

	if *writeSpeed <= 0 {
		*writeSpeed = 1e13
	} else {
		*writeSpeed = *writeSpeed * 1024 * 1024
	}

	workers := proxy.NewWorkers(*wokerNum, BUFFER_SIZE, MAX_READLOOP_COUNT)
	p, err := proxy.NewTCPProxy(workers, *bindAddr, splitedUpstreamAddr, 5, 30*time.Second, time.Duration(*upstreamCheckInterval)*time.Second,
		proxy.WithConcurrentLimit(*concurrentLimit),
		proxy.WithBandwidthLimit(float64(*readSpeed), float64(*writeSpeed)),
		proxy.WithDialTimeout(time.Duration(*dialTimeout)*time.Second),
		proxy.WithWriteTimeout(time.Duration(*writeTimeout)*time.Second),
	)
	if err != nil {
		util.Logger().Fatal("create proxy error: " + err.Error())
	}
	defer func() {
		util.Logger().Info("closing proxy...")
		p.Close()
		util.Logger().Info("closing proxy...done")
	}()
	for i := 0; i < *wokerNum; i++ {
		go workers[i].Run()
	}
	go p.Accepting()
	util.Logger().Info("proxy is running")
	<-ctrlc
}
