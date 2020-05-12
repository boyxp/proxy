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
	debug := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	if flag.NFlag() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	Log("服务器IP：", *ip)
	Log("服务器端口：", *port)

	//建立连接
	server := *ip+":"+strconv.Itoa(*port)

	conn, err := net.Dial("tcp", server)
	if err != nil {
		Log("连接失败:", err)
		return
	}

	//写入握手包
	req := []byte{5, 1, 0}
	conn.Write(req)

	//读取响应
	reader := bufio.NewReader(conn)

	ver, _ := reader.ReadByte()
	Log("响应版本：", ver)

	method, _ := reader.ReadByte()
	Log("响应method：", method)

	if *debug {
		heartbeat(conn)
	} else {
		wait(conn)
	}
}

func wait(conn net.Conn) {
	defer close(conn)

	buf := make([]byte, 8192)
	for {
		count, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				Log("服务器报错：", buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log("对方连接关闭")
			}

			break
		}

		if count > 0 {
			Log("用户请求：", string(buf[:count]))
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
				Log("对方连接关闭")
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