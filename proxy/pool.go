package proxy

import "net"
import "errors"
import "sync"

var locker sync.Mutex

type TcpPool struct {
	pool chan net.Conn
	count int
	max int
}

func (this *TcpPool) Init(max int) {
	this.pool  = make(chan net.Conn, max)
	this.count = 0
	this.max   = max
}

func (this *TcpPool) Get() (conn net.Conn, err error) {
	if this.count < 1 {
		err = errors.New("连接池没有可用TCP连接")
		return
	}

	locker.Lock()
	conn = <-this.pool
	this.count--
	locker.Unlock()
	return
}

func (this *TcpPool) Put(conn net.Conn) (res bool, err error) {
	if this.count >= this.max {
		err = errors.New("连接池已满")
		return
	}

	locker.Lock()
	this.pool<- conn
	this.count++
	locker.Unlock()
	return true, nil
}

func (this *TcpPool) Len() int {
	return this.count
}
