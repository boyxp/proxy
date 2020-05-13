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
	flag.Parse()

	if flag.NFlag() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	fmt.Println(*ip)
	fmt.Println(*port)

	//建立连接
	server := *ip+":"+strconv.Itoa(*port)

	conn, err := net.Dial("tcp", server)
	if err != nil {
		fmt.Println(err)
		return
	}

	//写入握手包
	req := []byte{5, 0, 0}
	conn.Write(req)

	//读取响应
	reader := bufio.NewReader(conn)

	ver, _ := reader.ReadByte()
	Log("版本：", ver)

	method, _ := reader.ReadByte()
	Log("method：", method)

	Log("写入模拟请求")

	//写入真实请求
	query(conn)
}

func query(conn net.Conn) {
	defer close(conn)

	buf := make([]byte, 8192)
	for i := 0;; i++ {
		conn.Write([]byte(strconv.Itoa(i)+"query"))

		count, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				Log("服务器报错：", buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log("设备连接关闭")
			}

			break
		}

		if count > 0 {
			Log("接收：", i, string(buf[:count]))
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