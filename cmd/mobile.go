package main

import "fmt"
import "flag"
import "os"
import "strconv"
import "net"
import "bufio"
import "time"
import "io"
import "proxy"

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
		Log(Now(), "连接失败：seq=", seq, "err=", err)
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
	Log(Now(), "响应版本：seq=", seq, "ver=", ver)

	status, _ := reader.ReadByte()
	Log(Now(), "响应status：seq=", seq, "status=", status)

	if debug {
		heartbeat(conn)
	} else {
		//wait(conn, seq)
		run(conn, seq)
	}
}

func run(client net.Conn, seq int) {
	defer client.Close()

	b := make([]byte, 1024)
	n, err := client.Read(b[:])

	var host,port string
	switch b[3] {
		case 0x01://IP V4
			host = net.IPv4(b[4],b[5],b[6],b[7]).String()
		case 0x03://域名
			host = string(b[5:n-2])//b[4]表示域名的长度
		case 0x04://IP V6
			host = net.IP{b[4], b[5], b[6], b[7], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15], b[16], b[17], b[18], b[19]}.String()
	}
	port = strconv.Itoa(int(b[n-2])<<8|int(b[n-1]))

	server, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		Log(Now(), "连接失败：seq=", seq, "err=", err)
		return
	}

	Log(Now(), "连接成功：seq=", seq, "host=", host, "port=", port)

	defer server.Close()
	client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})

	go CopyLocalToRemote(client, server, seq)
	go CopyRemoteToLocal(server, client, seq)
}


//转发用户数据到设备
func CopyLocalToRemote(input net.Conn, output net.Conn, seq int) {
	defer output.Close()

	remote  := output.RemoteAddr().String()
	traffic := 0

	buf := proxy.Leaky.Get()
	defer proxy.Leaky.Put(buf)

	for {
			count, err := input.Read(buf)
			if err != nil {
				if err == io.EOF && count > 0 {
					traffic += count
					output.Write(buf[:count])
				}

				if err == io.EOF  && count == 0 {
					Log(Now(), "用户主动断开：seq=", seq, "remote=", remote, "traffic_up=", traffic)
					return
				}

				break
			}

			if count > 0 {
				traffic += count
				_, err := output.Write(buf[:count])
				if err != nil {
					Log(Now(), "设备被动断开： seq=", seq, "remote=", remote, "traffic_up=", traffic)
					return
				}
			}
	}
}

//转发设备数据到用户
func CopyRemoteToLocal(input net.Conn, output net.Conn, seq int) {
	defer output.Close()

	remote  := input.RemoteAddr().String()
	traffic := 0

	buf := proxy.Leaky.Get()
	defer proxy.Leaky.Put(buf)

	for {
			count, err := input.Read(buf)
			if err != nil {
				if err == io.EOF && count > 0 {
					traffic += count
					output.Write(buf[:count])
				}

				if err == io.EOF  && count == 0 {
					Log(Now(), "设备主动断开：seq=", seq, "remote=", remote, "traffic_down=", traffic)
					return
				}

				break
			}

			if count > 0 {
				traffic += count
				_, err := output.Write(buf[:count])
				if err != nil {
					Log(Now(), "用户被动断开：seq=", seq, "remote=", remote, "traffic_down=", traffic)
					return
				}
			}
	}
}


func wait(conn net.Conn, seq int) {
	defer close(conn)

	buf := make([]byte, 8192)
	for {
		count, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF && count > 0 {
				Log(Now(), "服务器报错：seq=", seq, "res=", buf[:count])
			}

			if err == io.EOF  && count == 0 {
				Log(Now(), "用户连接关闭：seq=", seq)
			}

			break
		}

		if count > 0 {
			Log(Now(), "用户请求：seq=", seq, "data=", string(buf[:count]))
			conn.Write([]byte("recv"))
			Log(Now(), "请求响应：seq=", seq)
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

func Now() string {
	return time.Now().Format("2006-01-02T15:04:05.999999999Z07:00")
}