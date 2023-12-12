## Introduction

Evproxy is a simple high performance TCP(UDP not supported yet ) proxy based on epoll, which aims to adress the high memory usage problem under high concurrent connections(>100k) when the proxy is implemented in the GO stdnet. It supports basic source IP hash load balancing, read/write timeout, and bandwidth speed limiting.

## Benchmark

## Features

- [x]  ~10x memory usage compared to the go stdnet when connections > 100k.
- [x]  TCP support
- [x]  Source IP hash load balancing
- [x]  Basic connection and traffic statistics
- [ ]  UDP support
- [ ]  TLS support
- [ ]  Proxy protocol support

## Usage