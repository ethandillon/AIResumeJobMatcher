package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// AnalysisRequest is the same as before
type AnalysisRequest struct {
	Resume         string `json:"resume"`
	JobDescription string `json:"jobDescription"`
}

// NEW: A structured response for the dashboard
type AnalysisResponse struct {
	MatchScore   int    `json:"matchScore"`
	Improvements string `json:"improvements"`
	NextSteps    string `json:"nextSteps"`
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("Received analysis request. Resume length: %d, Job Description length: %d", len(req.Resume), len(req.JobDescription))

	// =======================================================================
	// FUTURE: Gemini API Integration Point
	// -----------------------------------------------------------------------
	// You would ask Gemini to return a JSON object matching the
	// AnalysisResponse struct. Most modern LLMs can do this reliably.
	//
	// Example prompt for Gemini:
	// `prompt := fmt.Sprintf(
	//     "Analyze the following resume against the job description.
	//      Return a JSON object with three keys: 'matchScore' (an integer from 0-100),
	//      'improvements' (a string with 3-5 bullet points for resume improvement),
	//      and 'nextSteps' (a string with 2-3 bullet points for next actions).
	//      Resume: %s\n\nJob Description: %s",
	//     req.Resume, req.JobDescription,
	// )`
	// `apiResponse, err := callGeminiAPI(prompt)`
	// `json.Unmarshal([]byte(apiResponse), &response)`
	// =======================================================================

	// MOCK ANALYSIS: Create a structured response.
	// We use `\n• ` for bullet points.
	improvementsText := "• **Keyword Optimization:** The job description mentions 'Cloud Deployment' and 'CI/CD pipelines'. Ensure these exact phrases are in your skills or project sections.\n" +
		"• **Quantify Achievements:** Instead of 'Improved system performance,' try 'Improved system performance by 15% by refactoring legacy code.'\n" +
		"• **Tailor Summary:** Rewrite your professional summary to directly address the needs mentioned in the job description."

	nextStepsText := "• **Apply on Company Website:** Submit your updated resume through the official careers page.\n" +
		"• **Network:** Find the hiring manager or a team member on LinkedIn and send a brief, polite message expressing your interest."

	response := AnalysisResponse{
		MatchScore:   78,
		Improvements: improvementsText,
		NextSteps:    nextStepsText,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func main() {
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)
	http.HandleFunc("/chat", chatHandler)

	port := "8080"
	log.Printf("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err)
	}
}
