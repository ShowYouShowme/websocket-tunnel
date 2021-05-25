package main

import (
	"encoding/binary"
	"flag"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"
)
const(
	SERVER_IP       = "172.18.80.199"
	SERVER_PORT     = 3128
	MTU = 1600
)

func wsWriteWorker(conn *websocket.Conn, wsCh chan []byte){
	for {
		select {
			case data := <-wsCh:
				err := conn.WriteMessage(websocket.TextMessage, data)
				if err != nil{
					panic(err)
				}
		}
	}
}

func echo(w http.ResponseWriter, r *http.Request) {
	log.Println("create a echo worker...")
	var upgrader = websocket.Upgrader{} // use default options
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	var wsCh = make(chan []byte)
	var dataCh = make(chan []byte) // echo 协程 用来给worker 发送消息
	go wsWriteWorker(c, wsCh)
	go worker(dataCh, wsCh)
	defer c.Close()
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		dataCh <- message
	}
}

// TCP 连接是阻塞的,因此放在协程里面处理
func startConnect(id uint32, content *[]byte, connectCh chan []interface{})  {
	address := SERVER_IP + ":" + strconv.Itoa(SERVER_PORT)
	conn, err := net.Dial("tcp", address)
	if err != nil{
		log.Println(err)
		return
	}
	data := make([]interface{}, 3)
	data[0] = id
	data[1] = conn
	data[2] = *content
	connectCh <- data
}

// 需要一个指令来通知worker删除记录
func producer(id uint32, wsCh chan []byte, from net.Conn, deleteCh chan []byte){
	buf := make([]byte, MTU)
	var len int
	var err error

	for{
		//conn.SetReadDeadline(time.Now().Add(time.Second * 20))
		if len,err = from.Read(buf); err != nil{
			log.Println("line 97 : ", err)
			var data []byte= make([]byte,4)
			binary.BigEndian.PutUint32(data,id)
			deleteCh <- data
			wsCh <- data
			return
		}
		var data []byte= make([]byte,4)
		binary.BigEndian.PutUint32(data,id)
		data = append(data, buf[:len]...)
		// ID + CONTENT
		wsCh <- data
	}
}

func worker(ch chan []byte, wsCh chan []byte)  {
	var mc = map[uint32]net.Conn{}
	connectCh := make(chan []interface{})

	deleteCh := make(chan []byte) // producer 来通知worker 删除记录
	for{
		select {
			case message := <- deleteCh:
				id := binary.BigEndian.Uint32(message)
				if client, ok := mc[id]; ok{
					client.Close()
					delete(mc, id)
				}
			case value := <-connectCh:
				id := value[0].(uint32)
				conn := value[1].(net.Conn)
				content := value[2].([]byte)
				if _,err := conn.Write(content); err != nil{ // TODO 发指令给worker,发送消息
					log.Println("write error : ", err)
					// 关闭和删除连接
					conn.Close()
					return
				}
				mc[id] = conn
				go producer(id, wsCh, conn, deleteCh)

			case message := <-ch:
				idBuf := message[:4]
				id := binary.BigEndian.Uint32(idBuf)
				content := message[4:]
				client, ok := mc[id]
				if len(message) == 4{
					if ok{
						client.Close()
						delete(mc, id)
					}else{
						// 重复发送关闭指令
					}
					continue
				}
				if ok{
					// 将数据写入到wsClient
					if _,err := client.Write(content); err != nil{
						log.Println("write error : ", err)
						// 关闭和删除连接
						client.Close()
						delete(mc, id)
					}
				}else{
					// 创建新的连接
					go startConnect(id, &content, connectCh)
				}
		}
	}
}

func main() {
	go func() {
		ticker := time.NewTicker(time.Second * 3)
		for _ = range ticker.C{
			runtime.GC()
			log.Println("num : ", runtime.NumGoroutine())
		}
	}()
	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/echo", echo)
	var addr = flag.String("addr", "0.0.0.0:8080", "http service address")
	log.Fatal(http.ListenAndServe(*addr, nil))
}
