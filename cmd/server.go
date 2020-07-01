package main

import "flag"
import "fmt"
import "net/http"
import "encoding/json"
import "net"
import "log"
import "bufio"
import "io"
import "time"
import "proxy"
import "syscall"
import "errors"
import "encoding/base64"
import "strings"
import "sync"
import _ "net/http/pprof"

var Debug bool
var Devices sync.Map
var Users sync.Map
var DPort string
var PoolCap int

func main() {
	//处理传入参数
	devicePort  := flag.String("dport", "8888", "设备连接端口")
	commandPort := flag.String("cport", "8080", "指令接收端口")
	tcpCap      := flag.Int("pcap", 200, "连接池容量")
	debug       := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	Debug   = *debug
	DPort   = *devicePort
	PoolCap = *tcpCap

    Log(Now(), "设备端口：", *devicePort)
    Log(Now(), "指令端口：", *commandPort)
    Log(Now(), "调试模式：", *debug)
    Log(Now(), "连接容量：", *tcpCap)

	//runtime.GOMAXPROCS(4)

	//监听设备请求
	go listenMobile(*devicePort)

	//监听指令请求
	http.HandleFunc("/api.php", httpHandler)
    http.ListenAndServe(":"+*commandPort, nil)
}






//指令处理==============================================================================
//响应结构体
type Resp struct {
	Errno int    `json:"errno"`
	Msg   string `json:"msg"`
	CPort int    `json:"c_port"`
	DPort string `json:"d_port"`
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
    Log(Now(), "命令：JSON=", string(decode))

    //解析请求json
    var IJson interface{}
	JErr := json.Unmarshal(decode, &IJson)
	if JErr != nil {
		http.Error(w, "json error", 400)
   		return
	}

	JOut  := IJson.(map[string]interface{})
	if _, ok := JOut["data"]; !ok {
   		http.Error(w, "json error", 400)
   		return
   	}

   	//读取设置
	data := JOut["data"].(map[string]interface{})

	//判断参数结构
	if _, ok := data["timeout"]; !ok {
   		http.Error(w, "json error", 400)
   		return
   	}

   	token   := data["token"].(string)
   	userId  := data["userId"].(string)
   	ip      := data["c_ip"].(string)
   	timeout := data["timeout"].(float64)


   	//==如果token已经创建过连接池则报错

   	//创建监听，随机分配端口
   	var rawIP string
   	if Debug==true {
   		rawIP = "0.0.0.0:1080"
   	} else {
   		rawIP = "0.0.0.0:0"
   	}

    tcpAddr, err := net.ResolveTCPAddr("tcp", rawIP)

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

	Log(Now(), "命令：token=", token, "userId=", userId, "c_port=", port, "c_ip=", ip, "timeout=", timeout)


	//创建连接池
	pool := proxy.TcpPool{}
	pool.Init(PoolCap)
	Devices.Store(token, pool)
	Log(Now(), "创建连接池：token=", token, "userId=", userId, "c_port=", port, "c_ip=", ip, "timeout=", timeout)


	//超时时间设定
	deadline := time.Now().Add(time.Duration(timeout)*time.Second)

	go func() {
		time.Sleep(time.Duration(timeout) * time.Second)
		Log(Now(), "用户监听超时：port=", port)

		//关闭端口监听
		listener.Close()

		//删除端口映射
		Users.Delete(port)

		//清理连接池残留连接
		ele , _ := Devices.Load(token)
		pool := ele.(proxy.TcpPool)
		var device net.Conn
		var len = pool.Len()
		for i:=0;i<=len;i++ {
			var err error
			device, err = pool.Get()
			if err == nil {
				Log(Now(), "超时清理设备：port=", port, "token=", token, "seq=", i)
				device.Close()
			}
		}

		//清理连接池映射
		Devices.Delete(token)

		Log(Now(), "超时清理设备完毕：port=", port, "token=", token, "len=", len)
	}()

	//启动异步监听
	go listenCustomer(deadline, listener, port, ip, userId)

	//存储端口到设备token映射
	Users.Store(port, token)

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
	remote := conn.RemoteAddr().String()

	Log(Now(), "设备-连接：remote=", remote)

	reader      := bufio.NewReader(conn)
	version, _  := reader.ReadByte()

	if version != 1 {
		Log(Now(), "设备-版本错误：已断开，remote=", remote)
		conn.Close()
		return
	}

	length, _   := reader.ReadByte()

	if length > 32 {
		Log(Now(), "设备-token超长：已断开，remote=", remote)
		conn.Close()
		return
	}

	auth        := make([]byte, length)
	io.ReadFull(reader, auth)
	token       := string(auth)

	conn.Write([]byte{1, 0})

	Log(Now(), "设备-握手：token=", token, "remote=", remote, "version=", version, "len=", length)

	//判断设备连接池是否存在,不存在则断开
	if _, ok := Devices.Load(token); !ok {
		Log(Now(), "设备-连接池不存在：token=", token, "remote=", remote)
		conn.Close()
		return
	}

	ele , _ := Devices.Load(token)
	pool    := ele.(proxy.TcpPool)
	len     := pool.Len()
	Log(Now(), "设备：token=", token, "remote=", remote, "len=", len, "cap=", PoolCap)
	if len >= PoolCap {
		Log(Now(), "设备-连接池已满：token=", token, "remote=", remote, "len=", len, "cap=", PoolCap)
		conn.Close()
		return
	}

	pool.Put(conn)

	Log(Now(), "统计：设备连接数=", pool.Len())

	if Debug == true {
		//go heartbeat(conn)
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
func listenCustomer(deadline time.Time, listener *net.TCPListener, port int, ip string, userId string) {
	for{
			conn,err := listener.AcceptTCP()
			if err != nil {
				Log(Now(), "用户监听超时关闭：userId=", userId, "port=", port)
				return
			}

			addr  := conn.RemoteAddr().String()
			check := strings.Contains(addr, ip)
			if check == false && Debug==false {
				Log(Now(), "用户-不在白名单：userId=", userId, "port=", port, "remote=", addr, "ip=", ip)
				conn.Close()
				continue
			}

			Log(Now(), "用户-在白名单：userId=", userId, "port=", port, "remote=", addr, "ip=", ip)

			conn.SetKeepAlive(true)
			conn.SetKeepAlivePeriod(5*time.Second)
			conn.SetReadDeadline(deadline)

			go handleCustomer(conn, port, userId)
	}
}

//用户握手
func handleCustomer(conn net.Conn, port int, userId string) {
	reader      := bufio.NewReader(conn)
	version, _  := reader.ReadByte()
	nmethods, _ := reader.ReadByte()
	methods     := make([]byte,nmethods)
	io.ReadFull(reader, methods)

	conn.Write([]byte{5, 0})

	Log(Now(), "用户-连接信息：userId=", userId, "port=", port, "remote=", conn.RemoteAddr().String(), "version=", version, "nmethods=", nmethods, "methods=", methods)

	//通过端口映射查找token
	if _, ok := Users.Load(port);!ok {
		Log("未找到端口映射记录")
		conn.Close()
		return
	}

	token , _ := Users.Load(port)
	Log(Now(), "用户-找到端口映射：userId=", userId, "port=", port, "token=", token)

	//通过token找到连接池
	if _, ok := Devices.Load(token);!ok {
		Log(Now(), "未找到映射连接池：userId=", userId, "port=", port)
		conn.Close()
		return
	}

	ele , _ := Devices.Load(token)
	pool    := ele.(proxy.TcpPool)

	//先读取设备连接
	var device net.Conn
	for {
		var err error
		device, err = pool.Get()
		if err != nil {
			Log(Now(), "用户-没有可用设备：userId=", userId, "port=", port)
			conn.Close()
			return
		}

		live := checkLive(device)
		if live != nil {
			Log(Now(), "用户-选定设备已断开：userId=", userId, "port=", port)
			device.Close()
			continue
		}

		break
	}

	Log(Now(), "用户<<---->>设备：userId=", userId, "port=", port, "user=", conn.RemoteAddr().String(), "token=", token, "device=", device.RemoteAddr().String())

	//转发用户请求到设备
    go CopyUserToMobile(conn, device, userId)

    //转发设备请求到用户
	go CopyMobileToUser(device, conn, userId)
}

//转发用户数据到设备
func CopyUserToMobile(input net.Conn, output net.Conn, userId string) {
	defer output.Close()

	user    := input.RemoteAddr().String()
	device  := output.RemoteAddr().String()
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

				Log(Now(), "用户主动断开：userId=", userId, "user=", user, "device=", device, "err=", err, "traffic_up=", traffic)

				break
			}

			if count > 0 {
				traffic += count
				_, err := output.Write(buf[:count])
				if err != nil {
					Log(Now(), "设备被动断开： userId=", userId, "user=", user, "device=", device, "traffic_up=", traffic)
					break
				}
			}
	}
}

//转发设备数据到用户
func CopyMobileToUser(input net.Conn, output net.Conn, userId string) {
	defer output.Close()

	user    := output.RemoteAddr().String()
	device  := input.RemoteAddr().String()
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

				Log(Now(), "设备主动断开：userId=", userId, "device=", device, "user=", user,  "err=", err, "traffic_down=", traffic)

				break
			}

			if count > 0 {
				traffic += count
				_, err := output.Write(buf[:count])
				if err != nil {
					Log(Now(), "用户被动断开：userId=", userId, "device=", device, "user=", user,  "traffic_down=", traffic)
					break
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

func Now() string {
	return time.Now().Format("2006-01-02T15:04:05.999999999Z07:00")
}
