package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv" // <-- NEW: Import for string conversion
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

// Constants for our rate limiting
const usageCookieName = "usage_count"
const maxUsageCount = 3

// Structs are unchanged
type AnalysisRequest struct {
	Resume         string `json:"resume"`
	JobDescription string `json:"jobDescription"`
}

type AnalysisResponse struct {
	MatchScore   int    `json:"matchScore"`
	Improvements string `json:"improvements"`
	NextSteps    string `json:"nextSteps"`
}

func chatHandler(model *genai.GenerativeModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		// --- NEW: Rate Limiting Logic ---
		var currentCount int = 0
		usageCookie, err := r.Cookie(usageCookieName)

		// If cookie exists, read its value
		if err == nil {
			count, convErr := strconv.Atoi(usageCookie.Value)
			if convErr == nil {
				currentCount = count
			}
		}

		// Check if the user has exceeded the limit
		if currentCount >= maxUsageCount {
			log.Printf("User has reached usage limit of %d. Blocking request.", maxUsageCount)
			http.Error(w, "You have reached your usage limit for today.", http.StatusTooManyRequests)
			return // Stop processing
		}
		// --- End of Rate Limiting Logic ---

		var req AnalysisRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		log.Println("Received request, generating prompt for Gemini...")
		prompt := fmt.Sprintf(`
			Analyze the following resume against the job description.
			Your response MUST be a valid JSON object. Do not include any text or markdown formatting before or after the JSON object.
			The JSON object must have the following keys and value types:
			- "matchScore": an integer between 0 and 100 representing the match percentage.
			- "improvements": a string containing 3-5 bullet points (using **word** for bolding) on how to improve the resume for this specific job.
			- "nextSteps": a string containing 2-3 bullet points (using **word** for bolding) on actionable next steps for the applicant.

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

		ctx := context.Background()
		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			log.Printf("Error generating content: %v", err)
			http.Error(w, "Failed to get analysis from AI model", http.StatusInternalServerError)
			return
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

		var analysisResp AnalysisResponse
		if err := json.Unmarshal([]byte(cleanedString), &analysisResp); err != nil {
			log.Printf("Error unmarshaling JSON from Gemini: %v", err)
			log.Printf("Cleaned response that failed to parse: %s", cleanedString)
			http.Error(w, "Failed to parse AI model response", http.StatusInternalServerError)
			return
		}

		log.Printf("Successfully received and parsed analysis. Match score: %d%%", analysisResp.MatchScore)

		// --- NEW: Set the updated cookie on successful response ---
		newCount := currentCount + 1
		newCookie := http.Cookie{
			Name:     usageCookieName,
			Value:    strconv.Itoa(newCount),
			Path:     "/",   // Make it available on the whole site
			MaxAge:   86400, // 24 hours in seconds
			HttpOnly: true,  // Recommended for security
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, &newCookie)
		// --- End of Cookie Setting ---

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(analysisResp); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}

// main function is unchanged
func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, will look for environment variables")
	}

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
		client, err = genai.NewClient(ctx)
	}

	if err != nil {
		log.Fatalf("Failed to create Gemini client: %v", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash-latest")
	log.Println("Gemini client initialized successfully.")

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
	http.HandleFunc("/chat", chatHandler(model))

	port := "8080"
	log.Printf("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err)
	}
}
