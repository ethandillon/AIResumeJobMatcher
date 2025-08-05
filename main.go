package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time" // Import for Redis TTL

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9" // Import Redis client
	"google.golang.org/api/option"
)

// A custom type that can unmarshal a JSON string OR a JSON array of strings
// into a Go slice of strings. This makes our code resilient to Gemini's
// inconsistent output format.
type FlexibleStringSlice []string

func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = []string{s}
		return nil
	}
	var sl []string
	if err := json.Unmarshal(data, &sl); err == nil {
		*f = sl
		return nil
	}
	return fmt.Errorf("could not unmarshal %q as either a string or a slice of strings", data)
}

// Structs for API communication
type AnalysisRequest struct {
	Resume         string `json:"resume"`
	JobDescription string `json:"jobDescription"`
}

type AnalysisResponse struct {
	MatchScore   int                 `json:"matchScore"`
	Improvements FlexibleStringSlice `json:"improvements"`
	NextSteps    FlexibleStringSlice `json:"nextSteps"`
}

// Helper function to get the user's real IP address.
func getIPAddress(r *http.Request) string {
	// Check proxy headers first, as they are more reliable.
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		return strings.Split(ip, ",")[0]
	}
	ip = r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}

	// If no proxy headers, fall back to RemoteAddr.
	// Use net.SplitHostPort to reliably separate the IP and port.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// If there's an error (e.g., no port in the address),
		// the RemoteAddr is likely just the IP itself.
		return r.RemoteAddr
	}
	return host
}

// chatHandler now accepts both the Gemini model and the Redis client.
func chatHandler(model *genai.GenerativeModel, rdb *redis.Client) http.HandlerFunc {
	const maxUsageCount = 3
	const rateLimitDuration = 24 * time.Hour

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx := context.Background()

		// --- Redis-based IP Rate Limiting ---
		ip := getIPAddress(r)

		// INCR is an atomic operation.
		currentCount, err := rdb.Incr(ctx, ip).Result()
		if err != nil {
			log.Printf("Error incrementing Redis key for IP %s: %v", ip, err)
			http.Error(w, "Could not process request", http.StatusInternalServerError)
			return
		}

		// If this is the first request (count is 1), set the expiration for the key.
		if currentCount == 1 {
			rdb.Expire(ctx, ip, rateLimitDuration)
		}

		// Check if the user has exceeded the limit
		if currentCount > maxUsageCount {
			log.Printf("IP %s has reached usage limit of %d. Blocking request.", ip, maxUsageCount)
			http.Error(w, fmt.Sprintf("You have reached the limit of %d requests per day.", maxUsageCount), http.StatusTooManyRequests)
			return
		}

		var req AnalysisRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		log.Printf("Received request from IP %s (Usage: %d/%d)", ip, currentCount, maxUsageCount)

		prompt := fmt.Sprintf(`
			Analyze the following resume against the job description.
			Your response MUST be a valid JSON object. Do not include any text or markdown formatting before or after the JSON object.
			The JSON object must have the following keys and value types:
			- "matchScore": an integer between 0 and 100 representing the match percentage.
			- "improvements": a JSON array of strings, where each string is a bullet point (using **word** for bolding) on how to improve the resume.
			- "nextSteps": a JSON array of strings, where each string is a bullet point (using **word** for bolding) on actionable next steps.

			Here is the data:

			**Resume:**
			---
			%s
			---

			**Job Description:**
			---
			%s
			---
		`, req.Resume, req.JobDescription)

		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			log.Printf("Error generating content: %v", err)
			http.Error(w, "Failed to get analysis from AI model", http.StatusInternalServerError)
			return
		}

		if len(resp.Candidates) > 0 {
			if resp.Candidates[0].FinishReason == genai.FinishReasonSafety {
				log.Println("WARNING: Gemini response was blocked due to safety settings.")
				http.Error(w, "The analysis was blocked by the content safety filter. This can happen due to sensitive information. Please try again with different text.", http.StatusBadRequest)
				return
			}
		}

		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			log.Println("Received empty response from Gemini")
			http.Error(w, "Received an empty response from the AI model", http.StatusInternalServerError)
			return
		}

		rawResponse := resp.Candidates[0].Content.Parts[0]
		jsonString := fmt.Sprintf("%v", rawResponse)

		cleanedString := strings.TrimSpace(jsonString)
		cleanedString = strings.TrimPrefix(cleanedString, "```json")
		cleanedString = strings.TrimPrefix(cleanedString, "```")
		cleanedString = strings.TrimSuffix(cleanedString, "```")
		cleanedString = strings.TrimSpace(cleanedString)

		log.Printf("Cleaned JSON response from Gemini: %s", cleanedString)

		var analysisResp AnalysisResponse
		if err := json.Unmarshal([]byte(cleanedString), &analysisResp); err != nil {
			log.Printf("Error unmarshaling JSON from Gemini: %v", err)
			log.Printf("Cleaned response that failed to parse: %s", cleanedString)
			http.Error(w, "Failed to parse AI model response", http.StatusInternalServerError)
			return
		}

		log.Printf("Successfully received and parsed analysis. Match score: %d%%", analysisResp.MatchScore)

		// The cookie-setting logic has been removed.

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(analysisResp); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, will look for environment variables")
	}

	// --- Initialize Redis Client ---
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379" // Default value if not set in .env
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0, // use default DB
	})

	// Ping Redis to check the connection
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	log.Println("Redis client connected successfully.")

	// --- Initialize Gemini Client ---
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Println("GEMINI_API_KEY not found. Attempting to use GOOGLE_APPLICATION_CREDENTIALS.")
		if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
			log.Fatal("You must set either GEMINI_API_KEY or GOOGLE_APPLICATION_CREDENTIALS.")
		}
	}

	ctx := context.Background()
	var client *genai.Client
	var err error

	if apiKey != "" {
		client, err = genai.NewClient(ctx, option.WithAPIKey(apiKey))
	} else {
		client, err = genai.NewClient(ctx) // For service account
	}

	if err != nil {
		log.Fatalf("Failed to create Gemini client: %v", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-2.5-flash")
	log.Println("Gemini client initialized successfully.")

	// --- Set up HTTP server ---
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
	// Pass both the Gemini model and the Redis client to the handler
	http.HandleFunc("/chat", chatHandler(model, rdb))

	port := "8080"
	log.Printf("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err)
	}
}
