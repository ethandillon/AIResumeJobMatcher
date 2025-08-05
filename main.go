package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings" // <-- MAKE SURE THIS IS IMPORTED

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

// --- Structs for API communication (these are unchanged) ---

type AnalysisRequest struct {
	Resume         string `json:"resume"`
	JobDescription string `json:"jobDescription"`
}

type AnalysisResponse struct {
	MatchScore   int    `json:"matchScore"`
	Improvements string `json:"improvements"`
	NextSteps    string `json:"nextSteps"`
}

// --- chatHandler now accepts the Gemini model ---

func chatHandler(model *genai.GenerativeModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		var req AnalysisRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		log.Println("Received request, generating prompt for Gemini...")

		// --- Construct the detailed prompt for the Gemini API ---
		prompt := fmt.Sprintf(`
			Analyze the following resume against the job description.
			Your response MUST be a valid JSON object. Do not include any text or markdown formatting before or after the JSON object.
			The JSON object must have the following keys and value types:
			- "matchScore": an integer between 0 and 100 representing the match percentage.
			- "improvements": a string containing 3-5 bullet points (using •) on how to improve the resume for this specific job.
			- "nextSteps": a string containing 2-3 bullet points (using •) on actionable next steps for the applicant.

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

		// --- Make the API call to Gemini ---
		ctx := context.Background()
		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			log.Printf("Error generating content: %v", err)
			http.Error(w, "Failed to get analysis from AI model", http.StatusInternalServerError)
			return
		}

		// --- Extract, clean, and parse the JSON response from Gemini ---
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			log.Println("Received empty response from Gemini")
			http.Error(w, "Received an empty response from the AI model", http.StatusInternalServerError)
			return
		}

		rawResponse := resp.Candidates[0].Content.Parts[0]
		jsonString := fmt.Sprintf("%v", rawResponse)

		// NEW: Robustly clean the string before parsing
		// 1. Trim leading/trailing whitespace (this is key!)
		cleanedString := strings.TrimSpace(jsonString)

		// 2. Trim common markdown code fences
		cleanedString = strings.TrimPrefix(cleanedString, "```json")
		cleanedString = strings.TrimPrefix(cleanedString, "```")
		cleanedString = strings.TrimSuffix(cleanedString, "```")

		// 3. Trim whitespace again in case the trimming left any
		cleanedString = strings.TrimSpace(cleanedString)

		// Unmarshal the CLEANED string into our struct
		var analysisResp AnalysisResponse
		if err := json.Unmarshal([]byte(cleanedString), &analysisResp); err != nil {
			log.Printf("Error unmarshaling JSON from Gemini: %v", err)
			// Log the cleaned string that failed, for better debugging
			log.Printf("Cleaned response that failed to parse: %s", cleanedString)
			http.Error(w, "Failed to parse AI model response", http.StatusInternalServerError)
			return
		}

		log.Printf("Successfully received and parsed analysis. Match score: %d%%", analysisResp.MatchScore)

		// --- Send the structured response to the frontend ---
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

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		// This block is for Service Account authentication
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
		// Use service account if no API key
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
