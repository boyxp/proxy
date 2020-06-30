package proxy

import "net"
import "errors"

type TcpPool struct {
	pool chan net.Conn
	max int
}

func (this *TcpPool) Init(max int) {
	this.pool  = make(chan net.Conn, max)
	this.max   = max
}

func (this *TcpPool) Get() (conn net.Conn, err error) {
	if len(this.pool) < 1 {
		err = errors.New("连接池没有可用TCP连接")
		return
	}

	conn = <-this.pool
	return
}

func (this *TcpPool) Put(conn net.Conn) (res bool, err error) {
	if len(this.pool) >= this.max {
		err = errors.New("连接池已满")
		return
	}

	this.pool<- conn
	return true, nil
}

func (this *TcpPool) Len() int {
	return len(this.pool)
}
