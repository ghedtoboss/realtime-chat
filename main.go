package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	_"github.com/mattn/go-sqlite3"
)

var db *sql.DB

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./chat.db")
	if err != nil {
		log.Fatalf("database bağlantı sorunu: %v", err)
	}

	createTable := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password TEXT NOT NULL
	);
	`

	_, err = db.Exec(createTable)
	if err != nil {
		log.Fatalf("tablo oluşturulurken sorun: %v", err)
	}
}

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

type User struct {
	ID       int
	Username string
	Password string
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

func RegisterUser(w http.ResponseWriter, r *http.Request) {

	var user User
	json.NewDecoder(r.Body).Decode(&user)

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Şifre hashlenmedi", http.StatusInternalServerError)
		return
	}
	user.Password = string(hashedPassword)

	//veritabanına kaydet
	_, err = db.Exec("INSERT INTO users (username, password) VALUES (?, ?)", user.Username, user.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)

}

func LoginUser(w http.ResponseWriter, r *http.Request) {
	var user User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var storedUser User
	err = db.QueryRow("SELECT * FROM users WHERE username = ?", user.Username).Scan(&storedUser.ID, &storedUser.Username, &storedUser.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	initDB()

	http.HandleFunc("/register", RegisterUser)
	http.HandleFunc("/login", LoginUser)
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", handleConnections)
	go handleMessages()
	log.Println("Starting server on port 8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("Could not start server: ", err)
	}
}
