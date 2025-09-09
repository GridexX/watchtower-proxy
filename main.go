package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
)

func main() {
	// Get environment variables
	webhookID := os.Getenv("WEBHOOK_ID")
	apiKey := os.Getenv("WATCHTOWER_API_KEY")
	port := os.Getenv("PORT")
	watchtowerURL := os.Getenv("WATCHTOWER_URL")

	if watchtowerURL == "" {
		log.Printf("WATCHTOWER_URL not set, defaulting to localhost:8080")
		watchtowerURL = "localhost:8080"
	}

	if watchtowerURL == "" {
		log.Printf("WATCHTOWER_URL not set, defaulting to /v1/update")
		watchtowerURL = "http://localhost:8080"
	} else {
		log.Printf("Using custom WATCHTOWER_URL: %s", watchtowerURL)
	}

	if webhookID == "" {
		log.Fatal("WEBHOOK_ID environment variable is required")
	}
	if apiKey == "" {
		log.Fatal("WATCHTOWER_API_KEY environment variable is required")
	}
	if port == "" {
		port = "3000" // default port
	}

	// Create router
	r := mux.NewRouter()

	// Health check endpoint
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	// Webhook proxy endpoint
	r.HandleFunc("/api/webhooks/{id}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		// Verify the webhook ID matches
		if id != webhookID {
			log.Printf("Invalid webhook ID received: %s", id)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Create request to Watchtower
		client := &http.Client{}

		// Build the full Watchtower URL
		watchtowerFullURL := watchtowerURL + "/v1/update"
		log.Printf("Forwarding to Watchtower endpoint: %s", watchtowerFullURL)

		req, err := http.NewRequest("POST", watchtowerFullURL, nil)
		if err != nil {
			log.Printf("Error creating request: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Add authorization header
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		// Forward original request headers (optional)
		for name, values := range r.Header {
			if strings.ToLower(name) != "authorization" {
				req.Header[name] = values
			}
		}

		// Copy request body if present
		if r.Body != nil {
			req.Body = r.Body
		}

		// Execute request
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error forwarding request to Watchtower: %v", err)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Log response details for debugging
		log.Printf("Watchtower response: Status=%d, Headers=%v", resp.StatusCode, resp.Header)

		// If we get a 404, provide helpful guidance
		if resp.StatusCode == 404 {
			log.Printf("404 Error - Check if Watchtower endpoint is correct. Current: %s", watchtowerFullURL)
			log.Printf("Common Watchtower endpoints: /v1/update, /api/update, /webhook")
		}

		// Copy response headers
		for name, values := range resp.Header {
			w.Header()[name] = values
		}

		// Set status code
		w.WriteHeader(resp.StatusCode)

		// Copy response body
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			log.Printf("Error copying response body: %v", err)
		}

		log.Printf("Webhook %s forwarded to Watchtower - Status: %d", id, resp.StatusCode)
	}).Methods("POST")

	log.Printf("Starting proxy server on port %s", port)
	log.Printf("Webhook endpoint: /api/webhooks/%s", webhookID)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
