package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// okuma ve yazma tampon boyutlar
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var clients = make(map[*websocket.Conn]*Writer) //Bağlı olan tüm websocket istemcilerini ve onların yazarlarını tutan bir map
var broadcast = make(chan []byte)               //Tüm istemcilere gönderilecek mesajları tutan kanal

type Writer struct { //yazar struct'ı
	conn    *websocket.Conn //websocket bağlantısı
	message chan []byte     //mesajı taşıyan kanal
}

// yeni bir writer oluşturur ve döndürür
func NewWriter(conn *websocket.Conn) *Writer {
	w := &Writer{
		conn:    conn,
		message: make(chan []byte),
	}
	go w.writePump()
	return w
}

// message kanalından mesajları alır ve websocket bağlantısına yazar
func (w *Writer) writePump() {
	for msg := range w.message {
		if err := w.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("Error writing message: %v", err)
			w.conn.Close()
			break
		}
	}
}

/*
Yeni websocket bağlantılarını yönetir.
ilk olarak http bağlantısını websocket bağlantısına yükseltir
yeni bir writer oluşturu ve clients mapine ekler
bağlantı açık oldupu sürece istemciden gelen mesajları okur ve broadcast kanalına gönderir
*/
func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatalf("Could not upgrade: %v", err)
	}
	defer ws.Close()

	writer := NewWriter(ws)
	clients[ws] = writer

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("Error reading message: %v", err)
			delete(clients, ws)
			close(writer.message)
			break
		}
		broadcast <- msg
	}
}

// broadcast kanalındaki mesajları alır ve tüm istemcilere gönderir
func handleMessages() {
	for {
		msg := <-broadcast
		for client, writer := range clients {
			select {
			case writer.message <- msg:
			default:
				log.Printf("Dropping message for client: %v", client)
			}
		}
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleConnections)
	go handleMessages()
	log.Println("Starting server on port 8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("Could not start server: ", err)
	}
}
