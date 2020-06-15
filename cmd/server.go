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

var Debug bool
var Devices = make(map[string]proxy.TcpPool)
var Users   = make(map[int]string)

func main() {
	//处理传入参数
	devicePort  := flag.String("dport", "8888", "设备连接端口")
	commandPort := flag.String("cport", "8080", "指令接收端口")
	debug       := flag.Bool("debug", false, "调试模式")
	flag.Parse()

	Debug = *debug

    Log("设备端口：", *devicePort)
    Log("指令端口：", *commandPort)
    Log("调试模式：", *debug)

	runtime.GOMAXPROCS(4)

	//监听设备请求
	go listenMobile(*devicePort)

	//监听指令请求
	http.HandleFunc("/command.do", httpHandler)
    http.ListenAndServe(":"+*commandPort, nil)
}






//指令处理==============================================================================
//响应结构体
type Resp struct {
	Errno int   `json:"errno"`
	Msg  string `json:"msg"`
	Port int    `json:"port"`
}

//指令处理
func httpHandler(w http.ResponseWriter, r *http.Request) {
	//没有请求体直接拒绝
	if r.Body == nil {
            http.Error(w, "Please send a request body", 400)
            return
    }


    //解析请求json
	query := make(map[string]interface{})
   	json.NewDecoder(r.Body).Decode(&query)

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



	//启动异步监听
	//quit := make(chan int)

	//go listen_customer(listener, quit, token)

	Users[port] = token

	Log("用户数：", len(Users))

	//返回结果
	w.Write(Res(200, "Success", port))
}

//指令返回json
func Res(Errno int, Msg  string, Port int) []byte {
	res := Resp{Errno:Errno, Msg:Msg, Port:Port}

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
