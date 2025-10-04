package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type DockerHubPayload struct {
	PushData struct {
		Tag string `json:"tag"`
	} `json:"push_data"`
	Repository struct {
		Name     string `json:"name"`
		RepoName string `json:"repo_name"`
	} `json:"repository"`
}

func main() {
	// Get environment variables
	webhookID := os.Getenv("WEBHOOK_ID")
	apiKey := os.Getenv("WATCHTOWER_API_KEY")
	port := os.Getenv("PORT")
	watchtowerURL := os.Getenv("WATCHTOWER_URL")
	watchOnlyForLatestTag := os.Getenv("WATCH_ONLY_FOR_LATEST_TAG")
	delaySecondsEnv := os.Getenv("DELAY_SECONDS")

	// Convert the watchOnlyForLatestTag to a boolean
	watchOnly := false
	if strings.ToLower(watchOnlyForLatestTag) == "true" {
		watchOnly = true
		log.Printf("DEBUG: Watch only for latest tag is ENABLED")
	} else {
		log.Printf("DEBUG: Watch only for latest tag is DISABLED - all tags will trigger updates")
	}

	// Parse delay seconds (default to 20)
	delaySeconds := 20
	if delaySecondsEnv != "" {
		if parsed, err := strconv.ParseInt(delaySecondsEnv, 10, 64); err == nil && parsed > 0 {
			delaySeconds = int(parsed)
		}
	}
	log.Printf("DEBUG: Delay before forwarding webhook: %d seconds", delaySeconds)

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
		log.Printf("DEBUG: Webhook ID validated successfully")

		// Read request body once
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("ERROR: Failed to read request body: %v", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Check tag validation if enabled
		var shouldForward = true
		var payload DockerHubPayload

		if watchOnly {
			log.Printf("DEBUG: Tag validation enabled - parsing request body")

			// Parse JSON payload
			if err := json.Unmarshal(body, &payload); err != nil {
				log.Printf("ERROR: Failed to parse JSON payload: %v", err)
				log.Printf("DEBUG: Raw payload: %s", string(body))
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}

			tag := payload.PushData.Tag
			repoName := payload.Repository.Name
			if repoName == "" {
				repoName = payload.Repository.RepoName
			}

			log.Printf("DEBUG: Parsed webhook - Repository: %s, Tag: %s", repoName, tag)

			// Check if tag is "latest"
			if tag != "latest" {
				log.Printf("DEBUG: Tag '%s' is not 'latest' - skipping webhook forward", tag)
				shouldForward = false

				// Respond with success but don't forward
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"message":"Webhook received but not forwarded - tag is not latest","tag":"` + tag + `"}`))
				return
			} else {
				log.Printf("DEBUG: Tag is 'latest' - will forward webhook asynchronously")
			}
		}

		// Copy headers we want to forward
		headersToForward := make(map[string][]string)
		for name, values := range r.Header {
			if strings.ToLower(name) != "authorization" {
				headersToForward[name] = values
			}
		}

		// Respond immediately with 201 Accepted
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"message":"Webhook received and queued for processing","webhook_id":"` + id + `"}`))
		log.Printf("DEBUG: Responded with 201 - processing webhook asynchronously")

		// Process webhook asynchronously if it should be forwarded
		if shouldForward {
			go func() {
				// Add delay before forwarding
				log.Printf("DEBUG: Starting %d second delay before forwarding webhook", delaySeconds)
				time.Sleep(time.Duration(delaySeconds) * time.Second)
				log.Printf("DEBUG: Delay completed - now forwarding webhook to Watchtower")

				// Create request to Watchtower
				client := &http.Client{
					Timeout: 30 * time.Second,
				}

				// Build the full Watchtower URL
				watchtowerFullURL := watchtowerURL + "/v1/update"
				log.Printf("DEBUG: Forwarding to Watchtower endpoint: %s", watchtowerFullURL)

				req, err := http.NewRequest("POST", watchtowerFullURL, strings.NewReader(string(body)))
				if err != nil {
					log.Printf("ERROR: Failed to create request: %v", err)
					return
				}

				// Add authorization header
				req.Header.Set("Authorization", "Bearer "+apiKey)
				req.Header.Set("Content-Type", "application/json")
				log.Printf("DEBUG: Added Authorization header and Content-Type")

				// Forward original request headers
				for name, values := range headersToForward {
					req.Header[name] = values
				}

				log.Printf("DEBUG: Executing request to Watchtower...")

				// Execute request
				resp, err := client.Do(req)
				if err != nil {
					log.Printf("ERROR: Failed to forward request to Watchtower: %v", err)
					return
				}
				defer resp.Body.Close()

				// Log response details for debugging
				log.Printf("DEBUG: Watchtower response - Status: %d, Headers: %v", resp.StatusCode, resp.Header)

				// If we get a 404, provide helpful guidance
				if resp.StatusCode == 404 {
					log.Printf("ERROR: 404 - Watchtower endpoint not found. Current URL: %s", watchtowerFullURL)
					log.Printf("DEBUG: Common Watchtower endpoints to try: /v1/update, /api/update, /webhook")
				}

				// Read response body for logging
				respBody, err := io.ReadAll(resp.Body)
				if err != nil {
					log.Printf("ERROR: Failed to read response body: %v", err)
				} else {
					log.Printf("DEBUG: Watchtower response body: %s", string(respBody))
				}

				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					log.Printf("SUCCESS: Webhook %s forwarded to Watchtower successfully - Status: %d", id, resp.StatusCode)
				} else {
					log.Printf("WARNING: Webhook %s forwarded but got non-success status: %d", id, resp.StatusCode)
				}
			}()
		}
	}).Methods("POST")

	log.Printf("Starting proxy server on port %s", port)
	log.Printf("Webhook endpoint: /api/webhooks/%s", webhookID)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
