package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"text/template"
)

func main() {
	http.HandleFunc("/", uploadFile)
	http.HandleFunc("/lists", listFiles)
	http.HandleFunc("/lists/", displayImage)
	http.ListenAndServe(":8443", nil)
}

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

func displayImage(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Path[len("/lists/"):] // Kivágjuk a fájlnevet az URL-ből
	filepath := filepath.Join("/mnt/data", filename)

	http.ServeFile(w, r, filepath) // A fájl kiszolgálása
}
