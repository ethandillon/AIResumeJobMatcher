package main

import (
	"context"
	"encoding/json"

	// "errors" // No longer needed
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	// "os/signal" // No longer needed
	"strings"
	// "syscall" // No longer needed
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"
	"google.golang.org/api/option"
)

// A custom type that can unmarshal a JSON string OR a JSON array of strings
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

// A struct to hold application-wide dependencies.
type application struct {
	logger *slog.Logger
	model  *genai.GenerativeModel
	rdb    *redis.Client
}

// Helper function to get the user's real IP address.
func getIPAddress(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		return strings.Split(ip, ",")[0]
	}
	ip = r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// chatHandler is now a method on the 'application' struct.
func (app *application) chatHandler(w http.ResponseWriter, r *http.Request) {
	const maxUsageCount = 3
	const rateLimitDuration = 24 * time.Hour

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := context.Background()
	ip := getIPAddress(r)

	currentCount, err := app.rdb.Incr(ctx, ip).Result()
	if err != nil {
		app.logger.Error("redis increment failed", "ip", ip, "error", err)
		http.Error(w, "Could not process request", http.StatusInternalServerError)
		return
	}

	if currentCount == 1 {
		app.rdb.Expire(ctx, ip, rateLimitDuration)
	}

	if currentCount > maxUsageCount {
		app.logger.Warn("rate limit exceeded", "ip", ip, "count", currentCount)
		http.Error(w, fmt.Sprintf("You have reached the limit of %d requests per day.", maxUsageCount), http.StatusTooManyRequests)
		return
	}

	var req AnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	app.logger.Info("received analysis request", "ip", ip, "usage", fmt.Sprintf("%d/%d", currentCount, maxUsageCount))

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

	geminiCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := app.model.GenerateContent(geminiCtx, genai.Text(prompt))
	if err != nil {
		app.logger.Error("gemini content generation failed", "error", err)
		http.Error(w, "Failed to get analysis from AI model", http.StatusInternalServerError)
		return
	}

	if len(resp.Candidates) > 0 {
		if resp.Candidates[0].FinishReason == genai.FinishReasonSafety {
			app.logger.Warn("gemini response blocked by safety filter", "ip", ip)
			http.Error(w, "The analysis was blocked by the content safety filter.", http.StatusBadRequest)
			return
		}
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		app.logger.Warn("received empty response from gemini", "ip", ip)
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

	app.logger.Info("cleaned json response from gemini", "response", cleanedString)

	var analysisResp AnalysisResponse
	if err := json.Unmarshal([]byte(cleanedString), &analysisResp); err != nil {
		app.logger.Error("failed to unmarshal json from gemini", "error", err, "raw_response", cleanedString)
		http.Error(w, "Failed to parse AI model response", http.StatusInternalServerError)
		return
	}

	app.logger.Info("successfully parsed analysis", "ip", ip, "matchScore", analysisResp.MatchScore)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(analysisResp); err != nil {
		app.logger.Error("failed to encode response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// Health check handler
func (app *application) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]string{
		"status":      "available",
		"environment": os.Getenv("APP_ENV"),
		"version":     "1.0.0",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// CORRECTED main function
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := godotenv.Load(); err != nil {
		logger.Info("no .env file found, using environment variables")
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, Password: os.Getenv("REDIS_PASSWORD"), DB: 0})
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	logger.Info("redis client connected")

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		logger.Info("GEMINI_API_KEY not found, attempting GOOGLE_APPLICATION_CREDENTIALS")
		if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
			logger.Error("you must set either GEMINI_API_KEY or GOOGLE_APPLICATION_CREDENTIALS")
			os.Exit(1)
		}
	}

	ctx := context.Background()
	var client *genai.Client
	var err error

	if apiKey != "" {
		client, err = genai.NewClient(ctx, option.WithAPIKey(apiKey))
	} else {
		client, err = genai.NewClient(ctx)
	}
	if err != nil {
		logger.Error("failed to create gemini client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash-latest")
	logger.Info("gemini client initialized")

	app := &application{
		logger: logger,
		model:  model,
		rdb:    rdb,
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir("./static"))
	mux.Handle("/", http.StripPrefix("/", fileServer))
	mux.HandleFunc("/chat", app.chatHandler)
	mux.HandleFunc("/healthz", app.healthCheckHandler)

	handler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, // TODO: Restrict in production
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}).Handler(mux)

	logger.Info("starting server", "addr", ":"+port)

	// Use a direct, blocking ListenAndServe call.
	err = http.ListenAndServe(":"+port, handler)
	if err != nil {
		logger.Error("server failed to start", "error", err)
		os.Exit(1)
	}
}
