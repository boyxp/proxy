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
	ip   := flag.String("ip", "127.0.0.1", "代理服务器IP地址")
	port := flag.Int("port", 9999, "代理服务器端口")
	num  := flag.Int("num", 100, "连接数")
	flag.Parse()

	if flag.NFlag() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	fmt.Println(*ip)
	fmt.Println(*port)

	for i:=0;i<*num;i++ {
		go connect(*ip, *port, i)
	}

	run := make(chan int)
	<- run
}

func connect(ip string, port int, seq int) {
	//建立连接
	server := ip+":"+strconv.Itoa(port)

	conn, err := net.Dial("tcp", server)
	if err != nil {
		Log(Now(), "连接失败：seq=", seq, "err=", err)
		return
	}

	//写入握手包
	req := []byte{5, 0, 0}
	conn.Write(req)

	//读取响应
	reader := bufio.NewReader(conn)

	ver, _ := reader.ReadByte()
	Log(Now(), "版本：seq=", seq, "ver=", ver)

	method, _ := reader.ReadByte()
	Log(Now(), "method：seq=", seq, "method=", method)

	Log(Now(), "写入模拟请求：seq=", seq)

	//写入真实请求
	query(conn, seq)
}

func query(conn net.Conn, seq int) {
	defer close(conn)

	buf := make([]byte, 8192)
	for i := 0;; i++ {
		conn.Write([]byte(strconv.Itoa(i)+"query"))

		Log(Now(), "发送：seq=", seq, "msg=", i)

		count, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				Log(Now(), "服务器报错：seq=", seq, "err=", buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log(Now(), "设备连接关闭：seq=", seq)
			}

			break
		}

		if count > 0 {
			Log(Now(), "接收：seq=", seq, "msg=", i, "data=", string(buf[:count]))
		}

		time.Sleep(1*time.Second)
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

func Now() string {
	return time.Now().Format("2006-01-02T15:04:05.999999999Z07:00")
}
