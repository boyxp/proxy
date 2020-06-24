package main

import "fmt"
import "flag"
import "os"
import "strconv"
import "net"
import "bufio"
import "time"
import "io"

func main() {
	//处理传入参数
	ip    := flag.String("ip", "127.0.0.1", "代理服务器IP地址")
	port  := flag.Int("port", 8888, "代理服务器端口")
	token := flag.String("token", "aaaabbbbccccdddd", "设备标识")
	debug := flag.Bool("debug", false, "调试模式")
	num   := flag.Int("num", 100, "连接数")
	flag.Parse()

	if flag.NFlag() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	Log("服务器IP：", *ip)
	Log("服务器端口：", *port)

	for i:=0;i<*num;i++ {
		go connect(*ip, *port, *token, *debug, i)
	}

	run := make(chan int)
	<- run
}

func connect(ip string, port int, token string, debug bool, seq int) {
	//建立连接
	server := ip+":"+strconv.Itoa(port)

	conn, err := net.Dial("tcp", server)
	if err != nil {
		Log("连接失败：seq=", seq, "err=", err)
		return
	}

	//写入token
	len   := len(token)

	req := []byte{1, uint8(len)}
	conn.Write(req)
	conn.Write([]byte(token))

	//读取响应
	reader := bufio.NewReader(conn)

	ver, _ := reader.ReadByte()
	Log("响应版本：seq=", seq, "ver=", ver)

	status, _ := reader.ReadByte()
	Log("响应status：seq=", seq, "status=", status)

	if debug {
		heartbeat(conn)
	} else {
		wait(conn, seq)
	}
}

func wait(conn net.Conn, seq int) {
	defer close(conn)

	buf := make([]byte, 8192)
	for {
		count, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				Log("服务器报错：seq=", seq, "res=", buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log("用户连接关闭：seq=", seq)
			}

			break
		}

		if count > 0 {
			Log("用户请求：seq=", seq, "data=", string(buf[:count]))
			conn.Write([]byte("recv"))
		}
	}
}

func heartbeat(conn net.Conn) {
	defer close(conn)

	buf := make([]byte, 8192)
	for i := 0; i <100; i++ {
		conn.Write([]byte("ping"))

		count, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				Log("服务器报错：", buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log("用户连接关闭")
			}

			break
		}

		if count > 0 {
			if count==4 {
				Log("心跳响应：", i, string(buf[:count]))
				time.Sleep(1*time.Second)
			} else {
				Log("用户请求：", string(buf[:count]))
				conn.Write([]byte("recv"))
			}
		}
	}
}

func close(conn net.Conn) {
	Log(conn.RemoteAddr().String(), time.Now().Format("2006-01-02 15:04:05"), "close\n")
	conn.Close()
}

func Log(v ...interface{}) {
    fmt.Println(v...)
    return
}