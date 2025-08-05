package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

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

const usageCookieName = "usage_count"
const maxUsageCount = 3

type AnalysisRequest struct {
	Resume         string `json:"resume"`
	JobDescription string `json:"jobDescription"`
}

type AnalysisResponse struct {
	MatchScore   int                 `json:"matchScore"`
	Improvements FlexibleStringSlice `json:"improvements"`
	NextSteps    FlexibleStringSlice `json:"nextSteps"`
}

func chatHandler(model *genai.GenerativeModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		var currentCount int = 0
		usageCookie, err := r.Cookie(usageCookieName)
		if err == nil {
			count, convErr := strconv.Atoi(usageCookie.Value)
			if convErr == nil {
				currentCount = count
			}
		}

		if currentCount >= maxUsageCount {
			log.Printf("User has reached usage limit of %d. Blocking request.", maxUsageCount)
			http.Error(w, "You have reached your usage limit for today.", http.StatusTooManyRequests)
			return
		}

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

		ctx := context.Background()
		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			log.Printf("Error generating content: %v", err)
			http.Error(w, "Failed to get analysis from AI model", http.StatusInternalServerError)
			return
		}

		// NEW: Check the Finish Reason
		if len(resp.Candidates) > 0 {
			if resp.Candidates[0].FinishReason == genai.FinishReasonSafety {
				log.Println("WARNING: Gemini response was blocked due to safety settings.")
				http.Error(w, "The analysis was blocked by the content safety filter. This can happen due to sensitive information in the resume or job description. Please try again with different text.", http.StatusBadRequest)
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

		// NEW: Unconditional Logging for Debugging
		log.Printf("Cleaned JSON response from Gemini: %s", cleanedString)

		var analysisResp AnalysisResponse
		if err := json.Unmarshal([]byte(cleanedString), &analysisResp); err != nil {
			log.Printf("Error unmarshaling JSON from Gemini: %v", err)
			log.Printf("Cleaned response that failed to parse: %s", cleanedString)
			http.Error(w, "Failed to parse AI model response", http.StatusInternalServerError)
			return
		}

		log.Printf("Successfully received and parsed analysis. Match score: %d%%", analysisResp.MatchScore)

		newCount := currentCount + 1
		newCookie := http.Cookie{
			Name:     usageCookieName,
			Value:    strconv.Itoa(newCount),
			Path:     "/",
			MaxAge:   86400,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, &newCookie)

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
