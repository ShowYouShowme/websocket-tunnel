package main

import (
	"encoding/binary"
	"flag"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/url"
	"runtime"
	"strconv"
	"time"
)

const (
	SERVER_IP   = "0.0.0.0"
	SERVER_PORT = 10006
	MTU         = 1600
)

const (
	CmdTypeStore   = "store"
	CmdTypeAcquire = "acquire"
	CmdTypeDelete  = "delete"
)

//func connCenter()  {
//	// TODO 这里可能会出现数据查找不到的情况
//	var m = map[uint32]net.Conn{}
//	for{
//		select {
//			case data, ok := <- cmdCh:
//				if ok == true{
//					if data.Type == CmdTypeAcquire{
//						d, ok := m[data.Id]
//						data.Ch <- ConnWrap{d, ok} // 当为空时会不会有问题
//					}else if data.Type == CmdTypeStore{
//						m[data.Id] = data.Conn
//					}else if data.Type == CmdTypeDelete{
//						delete(m, data.Id)
//					}
//				}else{
//					log.Println("exist from connCenter...")
//					return
//				}
//		}
//	}
//}

//// 使用channel来存和取数据 根据exist 判断是否存在
//func getConn(key uint32)(net.Conn, bool){
//	cmd := Cmd{CmdTypeAcquire,key,nil,make(chan ConnWrap)}
//	cmdCh <- cmd
//	ret := <-cmd.Ch
//	close(cmd.Ch)
//	return  ret.data, ret.exist
//}
//
//func storeConn(key uint32, value net.Conn)  {
//	cmd := Cmd{CmdTypeStore, key,value,nil}
//	cmdCh <- cmd // 存数据
//}
//
//func deleteConn(key uint32)  {
//	cmd := Cmd{CmdTypeDelete, key,nil,nil}
//	cmdCh <- cmd // 存数据
//}

var addr = flag.String("addr", "172.18.80.183:8080", "http service address")

func wsTunnel(remoteCh chan []byte, dataCh chan []byte) {
	log.SetFlags(0)
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/echo"}
	log.Printf("connecting to %s", u.String())
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			remoteCh <- message
		}
	}()

	for {
		data := <-dataCh
		err := c.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			log.Println("write:", err)
			return
		}
	}
}

//func handler(conn net.Conn, id uint32)  {
//	defer func() {
//		if err := recover(); err != nil{
//			log.Println("handler error : ", err)
//		}
//		conn.Close()
//
//		//通知 数据中心删除数据
//		deleteConn(id)
//
//		var data []byte= make([]byte,4)
//		binary.BigEndian.PutUint32(data,id)
//		ch <- data
//	}()
//
//	buf := make([]byte, MTU)
//	var n int
//	var err error
//	for{
//		// TODO 读取增加超时 -- 超时后通知对方来关闭
//		// TODO 为什么第二次读取就是EOF
//		// 写入channel TODO need to be tested
//		conn.SetReadDeadline(time.Now().Add(time.Second * 20))
//		if n, err = conn.Read(buf); err != nil{
//			panic(err)
//		}
//		var data []byte= make([]byte,4)
//		binary.BigEndian.PutUint32(data,id)
//		data = append(data, buf[:n]...)
//		ch <- data
//	}
//}

func worker(dataCh chan []byte, conn net.Conn, id uint32, remoteCh chan []byte) {
	// TODO MTU 设置为非常大会影响性能吗
	buf := make([]byte, MTU)
	var n int
	var err error
	for {
		// TODO 读取增加超时 -- 超时后通知对方来关闭
		//conn.SetReadDeadline(time.Now().Add(time.Second * 20))
		if n, err = conn.Read(buf); err != nil {
			log.Println(err)
			// TODO 这里要关闭连接吧 然后通知上层来删除记录
			// 利用我的Id通知上层删除记录
			var data []byte= make([]byte,4)
			binary.BigEndian.PutUint32(data,id)
			remoteCh <- data
			dataCh <- data
			return
		}
		var data = make([]byte, 4)
		binary.BigEndian.PutUint32(data, id)
		data = append(data, buf[:n]...)
		dataCh <- data
	}
}

func master(accept chan net.Conn, remoteCh chan []byte, dataCh chan []byte) {
	var index uint32 = 1
	var m = map[uint32]net.Conn{}
	for {
		select {
		case conn := <-accept:
			m[index] = conn
			go worker(dataCh, conn, index, remoteCh)
			index++
		case message := <-remoteCh:
			// 解析id 和 content 出来,然后转发到client
			idBuf := message[:4]
			id := binary.BigEndian.Uint32(idBuf)
			client, ok := m[id]
			if ok {
				if len(message) == 4 {
					client.Close() // 会导致对应的client 协程直接退出
					log.Println("close socket")
					delete(m, id)
				} else {
					if _, err := client.Write(message[4:]); err != nil {
						log.Println("line 73 : ", err)
						client.Close()
						delete(m, id)
					}
				}
			} else {
				log.Println("client ", id, " not exist!")
			}
		}
	}
}

// TODO ws 要增加心跳检测,出错重连
// TODO 非ws的连接如何断开
func main() {
	go func() {
		ticker := time.NewTicker(time.Second * 3)
		for _ = range ticker.C {
			runtime.GC()
			log.Println("num : ", runtime.NumGoroutine())
		}
	}()

	remoteCh := make(chan []byte)
	dataCh := make(chan []byte)
	go wsTunnel(remoteCh, dataCh)
	address := SERVER_IP + ":" + strconv.Itoa(SERVER_PORT)
	sock, err := net.Listen("tcp", address)
	if err != nil {
		panic(err)
	}
	var accept = make(chan net.Conn)
	go master(accept, remoteCh, dataCh)
	for {
		conn, err := sock.Accept()
		if err != nil {
			panic(err) // TODO 可以看看这里会出现什么错误
		}
		accept <- conn
	}
}
