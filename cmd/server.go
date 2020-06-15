package main

import "flag"
import "runtime"
import "fmt"
import "net/http"
import "encoding/json"
import "net"
import "log"
import "bufio"
import "io"
import "time"
import "proxy"
import "context"
import "syscall"
import "errors"
import "encoding/base64"
import "strings"

var Debug bool
var Devices = make(map[string]proxy.TcpPool)
var Users   = make(map[int]string)
var DPort string

func main() {
	//处理传入参数
	devicePort  := flag.String("dport", "8888", "设备连接端口")
	commandPort := flag.String("cport", "8080", "指令接收端口")
	debug       := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	Debug = *debug
	DPort = *devicePort

    Log("设备端口：", *devicePort)
    Log("指令端口：", *commandPort)
    Log("调试模式：", *debug)

	runtime.GOMAXPROCS(4)

	//监听设备请求
	go listenMobile(*devicePort)

	//监听指令请求
	http.HandleFunc("/api.php", httpHandler)
    http.ListenAndServe(":"+*commandPort, nil)
}






//指令处理==============================================================================
//响应结构体
type Resp struct {
	Errno int   `json:"errno"`
	Msg  string `json:"msg"`
	CPort int    `json:"c_port"`
	DPort string    `json:"d_port"`
}

//指令处理
func httpHandler(w http.ResponseWriter, r *http.Request) {
	base := r.PostFormValue("s")
	if len(base) == 0 {
		http.Error(w, "Please send a request body", 400)
		return
	}

	decode, err := base64.StdEncoding.DecodeString(base)
    if err != nil {
        http.Error(w, "error", 400)
        return
    }
    fmt.Println(string(decode))

    //解析请求json
    jr := strings.NewReader(string(decode))
	query := make(map[string]interface{})
   	json.NewDecoder(jr).Decode(&query)

   	if _, ok := query["data"]; !ok {
   		http.Error(w, "json error", 400)
   		return
   	}

   	data := query["data"].(map[string]interface{})
   	if _, ok := data["token"]; !ok {
   		http.Error(w, "json error", 400)
   		return
   	}

   	token  := data["token"].(string)
   	userId := data["userId"].(string)

   	//==如果token已经创建过连接池则报错


   	//创建监听，随机分配端口
    tcpAddr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
    if err != nil {
    	w.Write(Res(400, "端口解析失败", 0))
    	return
    }

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
    	w.Write(Res(400, "端口绑定失败", 0))
		return
	}

	port := listener.Addr().(*net.TCPAddr).Port

	Log("命令：token=", token, "userId=", userId, "port=", port)


	//创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	go func() {
		time.Sleep(60 * time.Second)
		Log("用户监听超时：port=", port)
		cancel()
		listener.Close()
		delete(Users, port)
	}()

	//启动异步监听
	go listenCustomer(ctx, listener, port)

	Users[port] = token

	Log("用户数：", len(Users))

	//返回结果
	w.Write(Res(200, "Success", port))
}

//指令返回json
func Res(Errno int, Msg  string, CPort int) []byte {
	res := Resp{Errno:Errno, Msg:Msg, CPort:CPort, DPort:DPort}

	raw, err := json.Marshal(&res)
    if err != nil {
        return []byte{}
    }

    return raw
}





//设备连接处理============================================================================

//设备监听
func listenMobile(port string) {
    addr := "0.0.0.0:"+port

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

		go handleMobile(conn)
	}
}

//设备处理
func handleMobile(conn net.Conn) {

	Log("设备-请求时间：", time.Now().Format("2006-01-02 15:04:05"))

	reader      := bufio.NewReader(conn)
	version, _  := reader.ReadByte()

	if version != 1 {
		Log("版本错误：已断开")
		conn.Close()
		return
	}

	length, _   := reader.ReadByte()
	auth        := make([]byte, length)
	io.ReadFull(reader, auth)
	token       := string(auth)

	Log("设备-连接信息：版本=", version, "len=", length, "token=", token)

	conn.Write([]byte{1, 0})

	Log("设备-连接地址：", conn.RemoteAddr().String())


	//判断设备连接池是否存在,不存在则初始化
	if _, ok := Devices[token]; !ok {
		pool := proxy.TcpPool{}
		pool.Init(20)
		Devices[token] = pool;
		Log("设备-创建连接池：", token)
	}

	pool := Devices[token]
	pool.Put(conn)
	Devices[token] = pool

	Log("设备-总连接数：", pool.Len())
	Log("设备-总设备数：", len(Devices))

	if Debug == true {
		go heartbeat(conn)
	}
}

//设备心跳
func heartbeat(conn net.Conn) (err error) {
	defer Close(conn)

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



//用户端连接处理===========================================================================
//用户监听
func listenCustomer(ctx context.Context, listener *net.TCPListener, port int) {
	for{
		select {
			case <-ctx.Done():
						Log("用户监听退出：port=", port)
						listener.Close()
						return;
			default    :
						conn,err := listener.AcceptTCP()
						if err != nil {
							Log("用户监听关闭：port=", port)
							continue
						}

						conn.SetKeepAlive(true)
						conn.SetKeepAlivePeriod(5*time.Second)

						go handleCustomer(ctx, conn, port)
		}
	}
}

//用户握手
func handleCustomer(ctx context.Context, conn net.Conn, port int) {

	Log("用户-请求时间：", time.Now().Format("2006-01-02 15:04:05"))

	reader      := bufio.NewReader(conn)
	version, _  := reader.ReadByte()
	nmethods, _ := reader.ReadByte()
	methods     := make([]byte,nmethods)
	io.ReadFull(reader, methods)

	conn.Write([]byte{5, 0})

	Log("用户-连接信息：版本=", version, "nmethods=", nmethods, "methods=", methods)

	Log("用户-连接地址：", conn.RemoteAddr().String())
	Log("用户-监听端口：", port)

	//通过端口映射查找token
	if _, ok := Users[port];!ok {
		Log("未找到端口映射记录")
		conn.Close()
		return
	}

	token := Users[port]
	Log("找到端口映射：port=", port, "token=", token)

	//通过token找到连接池
	if _, ok := Devices[token];!ok {
		Log("未找到映射连接池")
		conn.Close()
		return
	}

	pool := Devices[token]

	//先读取设备连接
	var device net.Conn
	for {
		var err error
		device, err = pool.Get()
		if err != nil {
			Log("用户-没有可用设备")
			conn.Close()
			return
		}

		live := checkLive(device)
		if live != nil {
			Log("用户-选定设备已断开")
			device.Close()
			continue
		}

		break
	}

	Log("用户-连接设备：", device.RemoteAddr().String())

	//向设备转发原始请求
    go CopyUserToMobile(ctx, conn, device)
	go CopyMobileToUser(ctx, device, conn)
}

//转发用户数据到设备
func CopyUserToMobile(ctx context.Context, input net.Conn, output net.Conn) {
	defer output.Close()

	buf := make([]byte, 8192)
	for {
		select {
			case <-ctx.Done():
						Log("用户转发到设备退出")
						input.Close()
						output.Close()
						return;
			default    :
						count, err := input.Read(buf)
						if err != nil {
							if err == io.EOF && count > 0 {
								output.Write(buf[:count])
							}

							if err == io.EOF  && count == 0 {
								Log("用户主动断开")
								return
							}

							break
						}

						if count > 0 {
							_, err := output.Write(buf[:count])
							if err != nil {
								Log("设备被动断开")
								return
							}
						}
		}
	}
}

//转发设备数据到用户
func CopyMobileToUser(ctx context.Context, input net.Conn, output net.Conn) {
	defer output.Close()

	buf := make([]byte, 8192)
	for {
		select {
			case <-ctx.Done():
						Log("设备转发到用户退出")
						input.Close()
						output.Close()
						return
			default    :

						count, err := input.Read(buf)
						if err != nil {
							if err == io.EOF && count > 0 {
								output.Write(buf[:count])
							}

							if err == io.EOF  && count == 0 {
								Log("设备主动断开")
								return
							}

							break
						}

						if count > 0 {
							_, err := output.Write(buf[:count])
							if err != nil {
								Log("用户被动断开")
								return
							}
						}
		}
	}
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





//全局方法==============================================================================

//断开连接
func Close(conn net.Conn) {
	Log("连接关闭：", conn.RemoteAddr().String(), "时间：", time.Now().Format("2006-01-02 15:04:05"), "close\n")
	conn.Close()
}

//日志打印
func Log(v ...interface{}) {
    fmt.Println(v...)
    return
}
