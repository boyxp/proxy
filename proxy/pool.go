package proxy

import "net"
import "errors"

type TcpPool struct {
	pool chan net.Conn
}

func (this *TcpPool) Init(cap int) {
	this.pool = make(chan net.Conn, cap)
}

func (this *TcpPool) Get() (conn net.Conn, err error) {
	select {
		case conn = <-this.pool :
		default 				:
								err = errors.New("连接池没有可用TCP连接")
	}

	return
}

func (this *TcpPool) Put(conn net.Conn) (res bool, err error) {
	select {
		case this.pool <- conn :
								res = true
		default                :
								err = errors.New("连接池已满")
	}

	return
}

func (this *TcpPool) Len() int {
	return len(this.pool)
}
