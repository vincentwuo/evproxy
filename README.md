## Introduction

Evproxy is a simple high performance TCP(UDP not supported yet ) proxy based on epoll, which aims to adress the high memory usage problem under high concurrent connections(>100k) when the proxy is implemented in the GO stdnet*. It supports basic source IP hash load balancing, read/write timeout, and bandwidth speed limiting.
   
## Features

- [x]  ~8x less memory usage compared to the go stdnet when connections > 100k.
- [x]  TCP support
- [x]  Source IP hash load balancing
- [x]  Basic connection and traffic statistics
- [ ]  UDP support
- [ ]  TLS support
- [ ]  Proxy protocol support

## Usage



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