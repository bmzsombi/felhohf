package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	http.HandleFunc("/", uploadFile)
	//http.HandleFunc("/", uploadFile)
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
