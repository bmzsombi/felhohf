package db

import (
	"encoding/json"
	"net/http"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	json.NewDecoder(r.Body).Decode(&creds)

	hash, err := HashPassword(creds.Password)
	if err != nil {
		http.Error(w, "Error hashing password", http.StatusInternalServerError)
		return
	}

	_, err = DB.Exec("INSERT INTO users (username, password_hash) VALUES ($1, $2)", creds.Username, hash)
	if err != nil {
		http.Error(w, "User already exists or error", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	var creds Credentials
	json.NewDecoder(r.Body).Decode(&creds)

	var hash string
	err := DB.QueryRow("SELECT password_hash FROM users WHERE username=$1", creds.Username).Scan(&hash)
	if err != nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	if !CheckPasswordHash(creds.Password, hash) {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := GenerateJWT(creds.Username)
	if err != nil {
		http.Error(w, "Could not generate token", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"token": token})
}
