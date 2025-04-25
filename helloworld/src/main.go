// @title File Upload API
// @version 1.0
// @description API for uploading and listing files
// @host localhost:8443
// @BasePath /
package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"text/template"

	auth "helloworld/db"
	_ "helloworld/docs" // Import the generated swagger docs

	httpSwagger "github.com/swaggo/http-swagger"
)

func main() {
	auth.InitDB()

	http.HandleFunc("/", uploadFile)
	http.HandleFunc("/lists", listFiles)
	http.HandleFunc("/lists/", displayImage)
	http.HandleFunc("/files/", serveFile)
	http.HandleFunc("/register", auth.RegisterHandler)
	http.HandleFunc("/login", auth.LoginHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Swagger UI
	http.Handle("/swagger/", httpSwagger.WrapHandler)

	http.ListenAndServe(":8443", nil)
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
func uploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Unable to get file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		filepath := filepath.Join("/mnt/data", header.Filename)
		// Create a new file in the PVC mount point
		out, err := os.Create(filepath) // A PVC mount point
		if err != nil {
			http.Error(w, "Unable to create file", http.StatusInternalServerError)
			return
		}
		defer out.Close()

		// Copy the uploaded file to the new file
		_, err = io.Copy(out, file)
		if err != nil {
			http.Error(w, "Unable to save file", http.StatusInternalServerError)
			return
		}

		w.Write([]byte("File uploaded successfully!"))
	} else {
		http.ServeFile(w, r, "static/index.html")
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
func displayImage(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Path[len("/lists/"):]
	filepath := filepath.Join("/mnt/data", filename)
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
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
