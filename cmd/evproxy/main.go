package main

import (
	"errors"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"evproxy"
	"evproxy/pkg/util"

	"github.com/spf13/viper"
)

const (
	BUFFER_SIZE        = 1024 * 32
	MAX_READLOOP_COUNT = 8
)

type config struct {
	WorkerNum   int
	ProxyConfig []proxyConfig
}
type proxyConfig struct {
	Type                  string
	BindAddr              string
	UpstreamAddrs         string
	UpstreamCheckInterval int
	ConcurrentLimit       int64
	ReadSpeed             int
	WriteSpeed            int
	DialTimeout           int
	WriteTimeout          int
}

var (
	proxyType             = flag.String("type", "tcp", "proxy type, defaut:tcp")
	wokerNum              = flag.Int("n", 0, "the number of workers to handlle the data transfer. default 0 will set it to the number of CPU cores")
	bindAddr              = flag.String("bind", "", "addr to accept downstream data. Example: 0.0.0.0:8890 ")
	upstreamAddrs         = flag.String("upstreams", "", "upstream addrs define where the data will be transfer to , need to be splited by comma. Example: 1.1.1.1:123, 2.2.2.2:123")
	upstreamCheckInterval = flag.Int("check", 30, "(unit:second) the interval to check whether the unreachable upstream endpoint is back online or not")
	concurrentLimit       = flag.Int64("c", 0, "cocurrent limit. '0' means no limit")
	readSpeed             = flag.Int("rmbps", 0, "mbyte per second for reading from the downstream. '0.0' means no limit")
	writeSpeed            = flag.Int("wmbps", 0, "mbyte per second for reading from the upstream. '0.0' means no limit")
	dialTimeout           = flag.Int("dtimeout", 15, "(unit:second) the timeout when dialing to the upstream")
	writeTimeout          = flag.Int("wtimeout", 30, "(unit:second) the timeout for the write operation when one of the peer is closed.")
	configFileDir         = flag.String("f", "", "config file dir.")
)

func main() {
	flag.Parse()
	ctrlc := make(chan os.Signal)
	signal.Notify(ctrlc, os.Interrupt)

	var proxies []*evproxy.Proxy

	//read configs from the config file
	if *configFileDir != "" {
		viper.SetConfigFile(*configFileDir)
		err := viper.ReadInConfig()
		if err != nil {
			util.Logger().Fatal("read config file error: " + err.Error())
		}
		var fileConfig config
		err = viper.Unmarshal(&fileConfig)
		if err != nil {
			util.Logger().Fatal("unmarshal config file error: " + err.Error())
		}
		if fileConfig.WorkerNum == 0 {
			fileConfig.WorkerNum = runtime.NumCPU()
		}
		workers := evproxy.NewWorkers(fileConfig.WorkerNum, BUFFER_SIZE, MAX_READLOOP_COUNT)
		for i := 0; i < fileConfig.WorkerNum; i++ {
			go workers[i].Run()
		}

		for _, proxyCnf := range fileConfig.ProxyConfig {
			p, err := setProxy(workers, proxyCnf.Type, proxyCnf.BindAddr, proxyCnf.UpstreamAddrs, proxyCnf.ConcurrentLimit, proxyCnf.ReadSpeed, proxyCnf.WriteSpeed, proxyCnf.DialTimeout, proxyCnf.WriteTimeout)
			if err != nil {
				util.Logger().Fatal("create proxy error: " + err.Error())
			}
			proxies = append(proxies, p)
		}

	} else {
		// read configs frome the command line
		if *wokerNum == 0 {
			*wokerNum = runtime.NumCPU()
		}
		workers := evproxy.NewWorkers(*wokerNum, BUFFER_SIZE, MAX_READLOOP_COUNT)
		for i := 0; i < *wokerNum; i++ {
			go workers[i].Run()
		}

		p, err := setProxy(workers, *proxyType, *bindAddr, *upstreamAddrs, *concurrentLimit, *readSpeed, *writeSpeed, *dialTimeout, *writeTimeout)
		if err != nil {
			util.Logger().Fatal("create proxy error: " + err.Error())
		}
		proxies = append(proxies, p)

	}

	for i := range proxies {
		util.Logger().Sugar().Info("proxy: ", proxies[i].Local(), " is running")
		go proxies[i].Accepting()
	}

	defer func() {
		for i := range proxies {
			util.Logger().Sugar().Info("closing proxy...: ", proxies[i].Local())
			proxies[i].Close()
		}
	}()

	<-ctrlc
}

func setProxy(workers []*evproxy.Worker, proxyType, bindAddr, upstreamAddrs string, concurrentLimit int64, readSpeed, writeSpeed, dialTimeout, writeTimeout int) (*evproxy.Proxy, error) {
	if proxyType != "tcp" {
		return nil, errors.New("proxy currently dosen't support type:" + proxyType)
	}

	if bindAddr == "" {
		return nil, errors.New("bind addr is empty")
	}
	splitedUpstreamAddr := strings.Split(upstreamAddrs, ",")
	for i := 0; i < len(splitedUpstreamAddr); i++ {
		splitedUpstreamAddr[i] = strings.TrimSpace(splitedUpstreamAddr[i])
	}
	if readSpeed <= 0 {
		readSpeed = 1e13
	} else {
		readSpeed = readSpeed * 1024 * 1024
	}

	if writeSpeed <= 0 {
		writeSpeed = 1e13
	} else {
		writeSpeed = writeSpeed * 1024 * 1024
	}

	p, err := evproxy.NewTCPProxy(workers, bindAddr, splitedUpstreamAddr, 5, 30*time.Second, time.Duration(*upstreamCheckInterval)*time.Second,
		evproxy.WithConcurrentLimit(concurrentLimit),
		evproxy.WithBandwidthLimit(float64(readSpeed), float64(writeSpeed)),
		evproxy.WithDialTimeout(time.Duration(dialTimeout)*time.Second),
		evproxy.WithWriteTimeout(time.Duration(writeTimeout)*time.Second),
	)
	return p, err
}
