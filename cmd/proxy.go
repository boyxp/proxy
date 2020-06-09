package main

import "fmt"
import "net"
import "log"
import "bufio"
import "io"
import "time"
import "proxy"
import "runtime"
import "flag"
import "syscall"
import "errors"


var pool proxy.TcpPool
var mport string
var cport string
var debug bool
var device net.Conn
var devices = make(map[string]net.Conn)

func main() {
	//处理传入参数
	mp := flag.String("mport", "8888", "设备连接端口")
	cp := flag.String("cport", "9999", "用户连接端口")
	de := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	mport = *mp
	cport = *cp
	debug = *de

    Log("设备端口：", mport)
    Log("用户端口", cport)
    Log("调试模式：", debug)

	runtime.GOMAXPROCS(4)

	pool = proxy.TcpPool{}
	pool.Init(500)

	go listen_mobile()
	listen_customer()
}

//用户端
func listen_customer() {
    addr := "0.0.0.0:"+cport

    tcpAddr, err := net.ResolveTCPAddr("tcp", addr)

    if err != nil {
        log.Fatalf("net.ResovleTCPAddr fail:%s", addr)
    }


	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatalf("listen %s fail: %s", addr, err)
		return
	}

	for {
		conn,err := listener.AcceptTCP()
		if err != nil {
			log.Fatal(err)
			continue
		}

		conn.SetKeepAlive(true)
		conn.SetKeepAlivePeriod(5*time.Second)

		go handle_customer(conn)
	}
}

func handle_customer(conn net.Conn) {

	Log("用户-请求时间：", time.Now().Format("2006-01-02 15:04:05"))

	reader      := bufio.NewReader(conn)
	version, _  := reader.ReadByte()
	nmethods, _ := reader.ReadByte()
	methods     := make([]byte,nmethods)
	io.ReadFull(reader, methods)

	conn.Write([]byte{5, 0})

	Log("用户-连接信息：版本=", version, "nmethods=", nmethods, "methods=", methods)

	Log("用户-连接地址：", conn.RemoteAddr().String())

	//先读取设备连接
	for {
		var err error
		device, err = pool.Get()
		if err != nil {
			Log("用户-没有可用设备")
			close(conn)
			return
		}

		live := checkLive(device)
		if live != nil {
			Log("用户-选定设备已断开")
			close(device)
			continue
		}

		break
	}

	Log("用户-连接设备：", device.RemoteAddr().String())

	//向设备转发原始请求
	defer conn.Close()
    go CopyUserToMobile(conn, device)
	CopyMobileToUser(device, conn)
}

//转发用户数据到设备
func CopyUserToMobile(input, output net.Conn) (err error) {
	buf := make([]byte, 8192)
	for {
		count, err := input.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				output.Write(buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log("用户主动断开")
			}

			break
		}

		if count > 0 {
			_, err := output.Write(buf[:count])
			if err != nil {
				Log("设备被动断开")
			}
		}
	}

	Log("设备连接断开")
	close(output)

	return
}

//转发设备数据到用户
func CopyMobileToUser(input, output net.Conn) (err error) {
	buf := make([]byte, 8192)
	for {
		count, err := input.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				output.Write(buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log("设备主动断开")
			}

			break
		}

		if count > 0 {
			_, err := output.Write(buf[:count])
			if err != nil {
				Log("用户被动断开")
			}
		}
	}

	Log("用户连接断开")
	close(output)

	return
}

//检查连接是否可用
var errUnexpectedRead = errors.New("unexpected read from socket")
func checkLive(conn net.Conn) error {
	var sysErr error

	sysConn, ok := conn.(syscall.Conn)
	if !ok {
		return nil
	}
	rawConn, err := sysConn.SyscallConn()
	if err != nil {
		return err
	}

	err = rawConn.Read(func(fd uintptr) bool {
		var buf [1]byte
		n, err := syscall.Read(int(fd), buf[:])
		switch {
		case n == 0 && err == nil:
			sysErr = io.EOF
		case n > 0:
			sysErr = errUnexpectedRead
		case err == syscall.EAGAIN || err == syscall.EWOULDBLOCK:
			sysErr = nil
		default:
			sysErr = err
		}
		return true
	})
	if err != nil {
		return err
	}

	return sysErr
}





//设备端
func listen_mobile() {
    addr := "0.0.0.0:"+mport

    tcpAddr, err := net.ResolveTCPAddr("tcp", addr)

    if err != nil {
        log.Fatalf("net.ResovleTCPAddr fail:%s", addr)
    }


	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatalf("listen %s fail: %s", addr, err)
		return
	}

	for {
		conn,err := listener.AcceptTCP()
		if err != nil {
			log.Fatal(err)
			continue
		}

		conn.SetKeepAlive(true)
		conn.SetKeepAlivePeriod(5*time.Second)

		go handle_mobile(conn)
	}
}

func handle_mobile(conn net.Conn) {

	Log("设备-请求时间：", time.Now().Format("2006-01-02 15:04:05"))

	reader      := bufio.NewReader(conn)
	version, _  := reader.ReadByte()
	length, _   := reader.ReadByte()
	auth        := make([]byte, length)
	io.ReadFull(reader, auth)
	token := string(auth)

	Log("设备-连接信息：版本=", version, "len=", length, "token=", token)

	conn.Write([]byte{1, 0})

	devices[token] = conn
	//pool.Put(conn)

	Log("设备-连接地址：", conn.RemoteAddr().String())
	Log("设备-总连接数：", len(devices))

	if debug == true {
		go heartbeat(conn)
	}
}

func heartbeat(conn net.Conn) (err error) {
	defer close(conn)

	buf := make([]byte, 8192)
	for {
		count, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				Log(buf[:count])
			}

			if err == io.EOF && count == 0 {
				Log("对方连接关闭")
			}

			break
		}

		if count > 0 {
			conn.SetReadDeadline(time.Now().Add(time.Duration(10)*time.Second))
			Log("设备心跳：", conn.RemoteAddr().String(), string(buf[:count]))
			conn.Write([]byte("pong"))
		}
	}
	return
}

func close(conn net.Conn) {
	Log("连接关闭：", conn.RemoteAddr().String(), "设备连接数：", pool.Len(), "时间：", time.Now().Format("2006-01-02 15:04:05"), "close\n")
	conn.Close()
}

func Log(v ...interface{}) {
    fmt.Println(v...)
    return
}
