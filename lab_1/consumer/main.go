package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	providerUrl   = os.Getenv("PROVIDER_URL")
	secretKey     = os.Getenv("SECRET_KEY")
	name          = os.Getenv("NAME")
	tokenLifetime = 600
)

func validateJWT(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, `{"detail":"Authorization header missing"}`, http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"detail":"Invalid token format"}`, http.StatusUnauthorized)
			return
		}
		tokenString := strings.Split(authHeader, " ")[1]
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(secretKey), nil
		})
		if err != nil {
			if ve, ok := err.(*jwt.ValidationError); ok && ve.Errors == jwt.ValidationErrorExpired {
				http.Error(w, `{"detail":"Token has expired"}`, http.StatusUnauthorized)
				return
			}
			http.Error(w, `{"detail":"Server error"}`, http.StatusInternalServerError)
			return
		}
		if token.Valid {
			ctx := context.WithValue(r.Context(), "payload", token.Claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			http.Error(w, `{"detail":"Invalid token"}`, http.StatusUnauthorized)
		}
	}
}

func randomizeJWT() string {
	currentDate := time.Now().Unix()
	payload := jwt.MapClaims{
		"user": uuid.New().String(),
		"iat":  currentDate,
		"exp":  currentDate + int64(tokenLifetime),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, payload)
	tokenString, err := token.SignedString([]byte(secretKey))
	if err != nil {
		fmt.Println("Error generating JWT:", err)
		return ""
	}
	return tokenString
}

func createTaskHandler(w http.ResponseWriter, r *http.Request) {
	totalStartTime := time.Now()

	inputData := r.URL.Query().Get("input_data")
	if inputData == "" {
		http.Error(w, `{"error":"Input data is required"}`, http.StatusBadRequest)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, `{"error":"Authorization header missing"}`, http.StatusUnauthorized)
		return
	}

	headers := map[string]string{
		"Authorization": authHeader,
		"Accept":        "application/json",
		"Content-Type":  "application/json",
	}

	bodyData := map[string]string{
		"input_data": inputData,
	}
	body, err := json.Marshal(bodyData)
	if err != nil {
		http.Error(w, `{"error":"Failed to encode JSON body"}`, http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest("POST", providerUrl, bytes.NewBuffer(body))
	if err != nil {
		http.Error(w, `{"error":"Failed to create request"}`, http.StatusInternalServerError)
		return
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{}
	startTime := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, `{"error":"Failed to connect to provider"}`, http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var responseBody map[string]interface{}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&responseBody); err != nil {
		http.Error(w, `{"error":"Failed to parse response"}`, http.StatusInternalServerError)
		return
	}

	requestTime := time.Since(startTime).Seconds()

	response := map[string]interface{}{
		"response":     responseBody,
		"request_time": requestTime,
		"consumer":     name,
		"input_data":   inputData,
		"total_time":   time.Since(totalStartTime).Seconds(),
	}

	fmt.Printf("Processed request for input_data=%s in %.6f seconds (Request to provider: %.6f seconds)\n", inputData, time.Since(totalStartTime).Seconds(), requestTime)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		fmt.Printf("Consumer: Error encoding final response: %v\n", err)
		http.Error(w, `{"error":"Failed to encode response"}`, http.StatusInternalServerError)
	}
}

func randomizeJWTHandler(w http.ResponseWriter, r *http.Request) {
	token := randomizeJWT()
	if token == "" {
		http.Error(w, `{"error":"Failed to generate JWT"}`, http.StatusInternalServerError)
		return
	}
	response := map[string]string{
		"token": token,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, `{"error":"Failed to encode response"}`, http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/create_task", validateJWT(createTaskHandler))
	http.HandleFunc("/randomize_jwt", randomizeJWTHandler)
	PORT := os.Getenv("PORT")
	if PORT == "" {
		PORT = "80"
	}
	fmt.Printf("Consumer service running on port %s\n", PORT)
	if err := http.ListenAndServe(":"+PORT, nil); err != nil {
		fmt.Printf("Consumer: Error starting server: %v\n", err)
	}
}
