package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
)

var (
	secretKey = os.Getenv("SECRET_KEY")
	name      = os.Getenv("NAME")
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
			next.ServeHTTP(w, r)
		} else {
			http.Error(w, `{"detail":"Invalid token"}`, http.StatusUnauthorized)
		}
	}
}

func computeHandler(w http.ResponseWriter, r *http.Request) {
	var requestData struct {
		InputData string `json:"input_data"`
	}
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		http.Error(w, `{"error":"Invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	inputData := requestData.InputData
	if inputData == "" {
		http.Error(w, `{"error":"Input data is required"}`, http.StatusBadRequest)
		return
	}

	number := new(big.Int)
	_, success := number.SetString(inputData, 10)
	if !success {
		http.Error(w, `{"error":"Incorrect 'input_data' parameter. It must be a valid number"}`, http.StatusBadRequest)
		return
	}

	startTime := time.Now()

	sum := new(big.Int) // Ініціалізуємо sum як 0
	currentTerm := new(big.Int).Set(number)

	for i := 0; i < 100; i++ {
		sum.Add(sum, currentTerm)
		currentTerm.Mul(currentTerm, big.NewInt(2))
	}

	modulus := new(big.Int).Exp(big.NewInt(3), big.NewInt(60), nil) // 3^60
	sum.Mod(sum, modulus)

	computationTime := time.Since(startTime).Seconds()

	fmt.Printf("Computed result for input_data=%s in %.6f seconds. Result modulo 3^60: %s\n", inputData, computationTime, sum.String())

	response := map[string]interface{}{
		"result":           sum.String(),
		"computation_time": computationTime,
		"name":             name,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, `{"error":"Failed to encode response"}`, http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/compute", validateJWT(computeHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
	fmt.Printf("Provider service running on port %s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("Provider: Server failed to start: %v\n", err)
	}
}
