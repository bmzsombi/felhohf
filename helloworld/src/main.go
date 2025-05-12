// @title File Upload API
// @version 1.0
// @description API for uploading and listing files
// @host localhost:8443
// @BasePath /
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	auth "helloworld/db"
	_ "helloworld/docs"
	"helloworld/kubeapi"

	"github.com/gorilla/websocket"
	httpSwagger "github.com/swaggo/http-swagger"
)

type App struct {
	KubeClient             *kubeapi.KubeClient
	PvcName                string
	Namespace              string
	UploadDir              string
	PodCompletionTimeout   time.Duration
	WsConnections          map[*websocket.Conn]bool // WebSocket kapcsolatok
	WsMutex                *sync.Mutex
	UploadNotificationChan chan string
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	auth.InitDB()

	kc, err := kubeapi.NewKubeClient()
	if err != nil {
		log.Fatalf("Failed to initialize KubeClient: %v", err)
	}

	app := &App{
		KubeClient:             kc,
		PvcName:                "detector-pvc",
		Namespace:              "detector",
		UploadDir:              "/mnt/data/",
		WsConnections:          make(map[*websocket.Conn]bool),
		WsMutex:                &sync.Mutex{},
		UploadNotificationChan: make(chan string),
	}

	go app.listenForUploadNotifications()

	http.HandleFunc("/", app.uploadFile)
	http.HandleFunc("/lists", listFiles)
	http.HandleFunc("/lists/", app.displayImage)
	http.HandleFunc("/files/", serveFile)
	http.HandleFunc("/register", auth.RegisterHandler)
	http.HandleFunc("/login", auth.LoginHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/ws", app.handleWebSocket)

	// Swagger UI
	http.Handle("/swagger/", httpSwagger.WrapHandler)

	http.ListenAndServe(":8443", nil)
}

func (a *App) listenForUploadNotifications() {
	log.Println("Upload notification listener started.")
	// Ez a ciklus addig fut, amíg a channel nyitva van és üzenetek érkeznek.
	// Ha az alkalmazásnak lenne szabályos leállítása, a channelt le kellene zárni.
	for notification := range a.UploadNotificationChan {
		log.Printf("[UPLOAD NOTIFICATION] %s", notification)
		// Itt további feldolgozás is történhetne, pl. WebSocket üzenet küldése, stb.
	}
	log.Println("Upload notification listener stopped.") // Ez csak akkor fut le, ha a channel lezárul.
}

func (a *App) messageHandler(key, value []byte) error {
	log.Printf("Üzenet feldolgozása: Key: %s, Value: %s\n", string(key), string(value))

	var msg map[string]string
	err := json.Unmarshal(value, &msg)
	if err != nil {
		log.Printf("Failed to unmarshal message value: %v", err)
		return err
	}
	a.WsMutex.Lock()
	for conn := range a.WsConnections {
		err = conn.WriteJSON(msg)
		if err != nil {
			log.Printf("Failed to send message to WebSocket connection: %v", err)
			delete(a.WsConnections, conn)
			conn.Close()
		}
	}
	a.WsMutex.Unlock()
	return nil
}

// @Summary Upload a File
// @Description Uploads a file to the server.
// @Accept multipart/form-data
// @Produce plain
// @Param file formData file true "File to upload"
// @Success 200 {string} string "File uploaded successfully"
// @Failure 400 {string} string "Bad request"
// @Failure 500 {string} string "Internal server error"
// @Router / [post]
func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Unable to get file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		filepath := filepath.Join("/mnt/data", header.Filename)
		out, err := os.Create(filepath)
		if err != nil {
			http.Error(w, "Unable to create file", http.StatusInternalServerError)
			return
		}
		defer out.Close()

		_, err = io.Copy(out, file)
		if err != nil {
			http.Error(w, "Unable to save file", http.StatusInternalServerError)
			return
		}

		_, err = a.KubeClient.CreatePod(header.Filename, a.PvcName, a.Namespace)
		if err != nil {
			log.Printf("Failed to create Kubernetes pod for file '%s': %v", header.Filename, err)
			http.Error(w, "File uploaded but failed to start processing job.", http.StatusInternalServerError)
			return
		}

		notificationMsg := fmt.Sprintf("File '%s' uploaded, by an other user", header.Filename)
		a.UploadNotificationChan <- notificationMsg
		w.Write([]byte("File uploaded successfully!"))
	} else {
		http.ServeFile(w, r, "static/login.html")
	}
}

// @Summary List Files
// @Description Returns a list of uploaded files.
// @Produce html
// @Success 200 {string} string "A list of files"
// @Failure 500 {string} string "Internal server error"
// @Router /lists [get]
func listFiles(w http.ResponseWriter, r *http.Request) {
	files, err := os.ReadDir("/mnt/data")
	if err != nil {
		http.Error(w, "Unable to read directory", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.ParseFiles("static/lists.html")
	if err != nil {
		http.Error(w, "Unable to parse template", http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, files)
	if err != nil {
		http.Error(w, "Unable to execute template", http.StatusInternalServerError)
		return
	}
}

// @Summary Display Image
// @Description Serves the requested image file.
// @Param filename path string true "The name of the file to display"
// @Produce octet-stream
// @Success 200 {file} file "The requested file"
// @Failure 404 {string} string "File not found"
// @Failure 500 {string} string "Internal server error"
// @Router /lists/{filename} [get]
func (a *App) displayImage(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Path[len("/lists/"):]
	path := filepath.Join("/mnt/data", filename)
	fileInfo, err := os.Stat(path)
	if os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if fileInfo.IsDir() {
		files, err := os.ReadDir(path)
		if err != nil {
			http.Error(w, "Failed to read directory", http.StatusInternalServerError)
			return
		}

		html := `
		<!DOCTYPE html>
		<html lang="en">
		<head>
			<meta charset="UTF-8">
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<title>` + filename + ` - Könyvtár</title>
		</head>
		<body>
			<a href="/lists" style="text-decoration: none; color: blue; font-size: 16px;">Vissza a listához</a>
			<h1>` + filename + ` - Könyvtár tartalma</h1>
			<ul>
		`
		for _, file := range files {
			if !strings.HasPrefix(file.Name(), ".") {
				link := filepath.Join("/lists", filename, file.Name())
				html += fmt.Sprintf(`<li><a href="%s">%s</a></li>`, link, file.Name())
			}
		}
		html += `
			</ul>
		</body>
		</html>
		`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
		return
	}

	html := `
	<!DOCTYPE html>
	<html lang="en">
	<head>
	    <meta charset="UTF-8">
	    <meta name="viewport" content="width=device-width, initial-scale=1.0">
	    <title>` + filename + ` - Kép</title>
	</head>
	<body>
	    <a href="/lists" style="text-decoration: none; color: blue; font-size: 16px;">Vissza a listához</a>
	    <h1>` + filename + ` - Kép</h1>
	    <img src="/files/` + filename + `" alt="` + filename + ` kép">
	</body>
	</html>
	`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// @Summary Serve File
// @Description Serves the actual file content.
// @Param filename path string true "The name of the file"
// @Produce octet-stream
// @Success 200 {file} file "The requested file"
// @Failure 404 {string} string "File not found"
// @Router /files/{filename} [get]
func serveFile(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Path[len("/files/"):]
	path := filepath.Join("/mnt/data", filename)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, path)
}

func (a *App) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	a.WsMutex.Lock()
	a.WsConnections[conn] = true
	a.WsMutex.Unlock()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			a.WsMutex.Lock()
			delete(a.WsConnections, conn)
			a.WsMutex.Unlock()
			log.Println("WebSocket connection closed:", err)
			break
		}
	}
}
