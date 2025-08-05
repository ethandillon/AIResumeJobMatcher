document.addEventListener("DOMContentLoaded", () => {
    // State
    let storedResume = "";

    // Overlay elements
    const resumeOverlay = document.getElementById("resume-overlay");
    const resumeInput = document.getElementById("resume-input");
    const saveResumeButton = document.getElementById("save-resume-button");
    const editResumeButton = document.getElementById("edit-resume-button");

    // Main form elements
    const jdForm = document.getElementById("jd-form");
    const jdInput = document.getElementById("jd-input");
    const runButton = document.getElementById("run-button");
    const runButtonText = document.getElementById("run-button-text");

    // Dashboard elements
    const dashboardPlaceholder = document.getElementById("dashboard-placeholder");
    const resultsContent = document.getElementById("results-content");
    const progressBarFill = document.getElementById("progress-bar-fill");
    const scoreText = document.getElementById("score-text");
    const improvementsContent = document.getElementById("improvements-content");
    const nextStepsContent = document.getElementById("next-steps-content");
    
    // --- Initial State ---
    resumeOverlay.classList.add("visible");

    // --- Event Listeners ---
    saveResumeButton.addEventListener("click", () => {
        storedResume = resumeInput.value;
        resumeOverlay.classList.remove("visible");
        editResumeButton.classList.remove("hidden");
    });

    editResumeButton.addEventListener("click", () => {
        resumeOverlay.classList.add("visible");
    });

    jdForm.addEventListener("submit", async (event) => {
        event.preventDefault();
        
        const jobDescriptionText = jdInput.value.trim();

        if (storedResume.trim() === "") {
            alert("Please enter your resume first by clicking the 'Edit Resume' button.");
            return;
        }
        if (jobDescriptionText === "") {
            alert("Please paste a job description.");
            return;
        }

        runButton.disabled = true;
        runButtonText.textContent = "Analyzing...";
        resultsContent.classList.add("hidden");
        dashboardPlaceholder.classList.remove("hidden");
        dashboardPlaceholder.innerHTML = "<p>Analyzing... this may take a moment.</p>";

        try {
            const response = await fetch("/chat", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ 
                    resume: storedResume, 
                    jobDescription: jobDescriptionText 
                }),
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`HTTP error! status: ${response.status}, message: ${errorText}`);
            }
            
            const data = await response.json();
            updateDashboard(data);

        } catch (error) {
            console.error("Error fetching analysis:", error);
            dashboardPlaceholder.innerHTML = `<p style="color: #ff5555;">An error occurred. Please check the console and try again.</p>`;
        } finally {
            runButton.disabled = false;
            runButtonText.textContent = "Analyze";
        }
    });

    /**
     * NEW: Converts a string with **bold** markdown into a string with <strong> tags.
     * @param {string} text - The input text from the API.
     * @returns {string} - The HTML-formatted string.
     */
    function markdownToHtml(text) {
      if (!text) return "";
      return text.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
    }

    /**
     * UPDATED: Populates the dashboard with data from the API.
     * @param {object} data - The parsed JSON response.
     */
    function updateDashboard(data) {
        dashboardPlaceholder.classList.add("hidden");
        resultsContent.classList.remove("hidden");

        progressBarFill.style.width = `${data.matchScore}%`;
        scoreText.textContent = `${data.matchScore}%`;

        // Use the markdown converter and innerHTML to render bold text
        improvementsContent.innerHTML = markdownToHtml(data.improvements);
        nextStepsContent.innerHTML = markdownToHtml(data.nextSteps);
    }
});