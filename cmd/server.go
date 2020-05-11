package main

import "fmt"
import "net"
import "log"
import "bufio"
import "io"
import "time"
import "proxy"
import "runtime"


var pool proxy.TcpPool

func main() {
	runtime.GOMAXPROCS(4)

	pool = proxy.TcpPool{}
	pool.Init(100)

	go listen_mobile()
	listen_customer()
}

//用户端
func listen_customer() {
    addr := "0.0.0.0:9999"
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

		//conn.SetReadDeadline(time.Now().Add(time.Duration(200)*time.Second))
		conn.SetKeepAlive(true)
		conn.SetKeepAlivePeriod(5*time.Second)

		go handle_customer(conn)
	}
}

func handle_customer(conn net.Conn) {

	Log("请求时间：", time.Now().Format("2006-01-02 15:04:05"))

	reader := bufio.NewReader(conn)

	ver, _ := reader.ReadByte()
	Log("版本：", ver)

	nmethods, _ := reader.ReadByte()
	Log("nmethods：", nmethods)

	methods := make([]byte,nmethods)
	io.ReadFull(reader, methods)
	Log("methods：", methods)

	res := []byte{5, 0}
	conn.Write(res)

	Log("用户连接：", conn.RemoteAddr().String())

	//先读取设备连接
	device, err := pool.Get()
	if err != nil {
		Log("没有可用设备")
		close(conn)
		return
	}


/*
	live := check(device)
	if live == false {
		Log("选定设备已断开")
		close(conn)
		return
	}
*/
	Log("连接设备：", device.RemoteAddr().String())

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
			output.Write(buf[:count])
		}
	}

	Log("设备连接释放")
	pool.Put(output)

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
			output.Write(buf[:count])
		}
	}
	return
}

func check(conn net.Conn) bool {

	buf := make([]byte, 1)

	count, err := conn.Read(buf)
	if err != nil {
		if err == io.EOF && count > 0 {
			Log(buf[:count])
		}

		if err == io.EOF && count == 0 {
			Log("对方连接关闭")
		}

		close(conn)

		return false
	}

	return true
}





//设备端
func listen_mobile() {
    addr := "0.0.0.0:8888"
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

		//conn.SetReadDeadline(time.Now().Add(time.Duration(200)*time.Second))
		conn.SetKeepAlive(true)
		conn.SetKeepAlivePeriod(5*time.Second)

		go handle_mobile(conn)
	}
}

func handle_mobile(conn net.Conn) {

	Log("设备-请求时间：", time.Now().Format("2006-01-02 15:04:05"))

	reader := bufio.NewReader(conn)

	ver, _ := reader.ReadByte()
	Log("设备-版本：", ver)

	nmethods, _ := reader.ReadByte()
	Log("设备-nmethods：", nmethods)

	methods := make([]byte,nmethods)
	io.ReadFull(reader, methods)
	Log("设备-methods：", methods)

	res := []byte{5, 0}
	conn.Write(res)

	pool.Put(conn)

	Log("设备连接数：", pool.Len())

	//go heartbeat(conn)
}

/*
开启心跳：循环读取，并相应，判断停止标志并退出
停止心跳：写入ready命令，等待ok回应后标记停止心跳，开始拷贝数据
拷贝数据：期间服务器停止接收心跳，单纯转发


设备类
	心跳监测 {
		读取，检测断开则踢出连接池
	}

	拷贝数据 {
		go 用户到设备
		go 设备到用户
	}

	释放设备 {
		开启心跳
		放入连接池
	}
*/

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
	Log("连接关闭：", conn.RemoteAddr().String(), time.Now().Format("2006-01-02 15:04:05"), "close\n")
	conn.Close()
}

func Log(v ...interface{}) {
    fmt.Println(v...)
    return
}
