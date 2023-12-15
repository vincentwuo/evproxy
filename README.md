## Introduction

Evproxy is a simple high performance TCP(UDP not supported yet ) proxy based on epoll(current linux only), which aims to adress the high memory usage problem under high concurrent connections(>100k) when the proxy is implemented in the GO stdnet*. It supports basic source IP hash load balancing, read/write timeout, and bandwidth speed limiting.
   
## Features

- [x]  ~8x less memory usage compared to the go stdnet when connections > 100k.
- [x]  TCP support
- [x]  Source IP hash load balancing
- [ ]  Basic connection and traffic statistics
- [ ]  UDP support
- [ ]  TLS support
- [ ]  Proxy protocol support

## Build

  ```bash
  go install github.com/vincentwuo/evproxy/cmd/evproxy@latest
  ```

## Download
Please check releases.

## Quick start
Create a simple tcp proxy

```bash
#example
#simple
evproxy -bind 10.0.1.3:5201 -upstreams 10.0.1.4:5201

#load balance
evproxy -bind 10.0.1.3:5201 -upstreams 10.0.1.4:5201,10.0.1.5:5201

#help
evproxy -h
	-type string
        proxy type, defaut:tcp (default "tcp")
	-bind string
        addr to accept downstream data. Example: 0.0.0.0:8890 
  -c int
        cocurrent limit. '0' means no limit
  -check int
        (unit:second) the interval to check whether the unreachable upstream endpoint is back online or not (default 30)
  -dtimeout int
        (unit:second) the timeout when dialing to the upstream (default 15)
  -f string
        config file dir.
  -n int
        the number of workers to handlle the data transfer. default 0 will set it to the number of CPU cores
  -rmbps int
        mbyte per second for reading from the downstream. '0.0' means no limit
  -upstreams string
        upstream addrs define where the data will be transfer to , need to be splited by comma. Example: 1.1.1.1:123, 2.2.2.2:123
  -wmbps int
        mbyte per second for reading from the upstream. '0.0' means no limit
  -wtimeout int
        (unit:second) the timeout for the write operation when one of the peer is closed. (default 30)
```

Create with JSON config file

```bash
evproxy -f path/to/config.json
```

Config file example

```json
{
    "workernum": 16,
    "proxyconfig": [
        {
            "type": "tcp",
            "bindaddr": "10.0.1.3:8080",
            "upstreamaddrs": "10.0.1.4:8080,10.0.1.5:8080",
            "upstreamcheckinterval": 30,
            "concurrentlimit": 10000,
            "readspeed": 10,
            "writespeed": 10,
            "dialtimeout": 60,
            "writetimeout":60
        },
        {
            "type": "tcp",
            "bindaddr": "10.0.1.3:8081",
            "upstreamaddrs": "10.0.1.14:8081",
            "upstreamcheckinterval": 30,
            "concurrentlimit": 10000,
            "readspeed": 10,
            "writespeed": 10,
            "dialtimeout": 60,
            "writetimeout":60
        }
        
    ]
}
```

## Benchmark

Spec:  

```bash
Ubuntu 18.04.3
Intel(R) Xeon(R) CPU           E5620  @ 2.40GH
16GB RAM
```

- **wrk 25000 connections for 60 seconds**
    
    Evproxy:
    
    ```bash
    wrk -t12 -c 25000 -d60  http://10.0.1.3:5201/index.html
    Running 1m test @ http://10.0.1.3:5201/index.html
      12 threads and 25000 connections
      Thread Stats   Avg      Stdev     Max   +/- Stdev
        Latency   199.37ms   34.13ms 746.51ms   78.96%
        Req/Sec    10.48k     2.23k   68.46k    90.88%
      7008919 requests in 1.00m, 4.58GB read
    Requests/sec: 116622.96
    Transfer/sec:     78.12MB
    ```
    
    Go stdnet proxy*:
    
    ```bash
    wrk -t12 -c 25000 -d60  http://10.0.1.3:5201/index.html
    Running 1m test @ http://10.0.1.3:5201/index.html
      12 threads and 25000 connections
      Thread Stats   Avg      Stdev     Max   +/- Stdev
        Latency   179.22ms   76.40ms   1.44s    81.14%
        Req/Sec    11.69k     3.13k   61.55k    80.50%
      7817582 requests in 1.00m, 5.14GB read
    Requests/sec: 130087.08
    Transfer/sec:     87.52MB
    ```
    
    The throughput of the go stdnet implementation is better than Evproxy When the number of connections is not too large. So if the load is not too heavy or ram is not the limit, the go stdnet is the better choice.
    
- **tcp echo test, 200K connections  packet size 800B, keeping sending at random(1, 10) seconds.**
    
    Evproxy:
    
    ram consumption is about 1.18GB 
    
    ```bash
    #top
    VIRT    RES    SHR S  %CPU  %MEM                                
    10.5g  109932  5824 S 453.0  0.7 
    ```
    
    Go stdnet proxy*:
    
    ram consumption is about 7.86GB 
    
    ```bash
    #top
    VIRT    RES    SHR S  %CPU  %MEM                                
    19.6g   6.7g   6048 S 615.6 43.1
    ```
    
    Evproxy shows better memory and cpu utilization when the number of connections is 200K. 

## Go stdnet proxy

Proxy implementation using the go standard package.

```go
//example
{
	listener,_:= net.Listen(addr)
	for{
	    conn, _:= listener.Accept()
		go handle(conn)
	}
}

//copy and traffic shaping
func copy(dst, src net.Conn, buf []byte)

func handle(conn net.Conn){
    upstreamConn,_ := net.Dial(upstreamAddr)
    
    //use buffer from the buffer pool
	  //usually we allocate a 16KB buf for each read and write
    //the problem is when the connection is idle, there is no way
    //to reuse these bufs, which leads to non-trivial memory consumption
    //when the number of connections is large.
    //i.e, 100k conncetion = 100k * (8k from 2 gorutines + 32KB buf) = 3.8GB  
	readBuf := pool.Get()
	writeBuf := pool.Get()

	for{
	    copy(upstreamConn, conn, readBuf)
      go copy(conn, upstreamConn, writeBuf)
	}
    
    pool.Put(readBuf)
    pool.Put(writeBuf)
}

```