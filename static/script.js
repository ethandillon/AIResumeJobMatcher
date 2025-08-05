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

    // NEW: Character Counter elements
    const resumeCounter = document.getElementById("resume-char-counter");
    const resumeCurrentChars = document.getElementById("resume-current-chars");
    const jdCounter = document.getElementById("jd-char-counter");
    const jdCurrentChars = document.getElementById("jd-current-chars");

    // Dashboard elements (unchanged)
    const dashboardPlaceholder = document.getElementById("dashboard-placeholder");
    const resultsContent = document.getElementById("results-content");
    const progressBarFill = document.getElementById("progress-bar-fill");
    const scoreText = document.getElementById("score-text");
    const improvementsContent = document.getElementById("improvements-content");
    const nextStepsContent = document.getElementById("next-steps-content");
    
    // --- Initial State ---
    resumeOverlay.classList.add("visible");

    // --- NEW: Character Counter Logic ---
    const setupCounter = (textArea, currentEl, counterEl) => {
        const update = () => {
            const currentLength = textArea.value.length;
            const maxLength = textArea.maxLength;
            currentEl.textContent = currentLength;

            counterEl.classList.remove("warning", "limit-reached");
            if (currentLength >= maxLength) {
                counterEl.classList.add("limit-reached");
            } else if (currentLength >= maxLength * 0.9) {
                counterEl.classList.add("warning");
            }
        };
        // Update on input, paste, cut, etc.
        textArea.addEventListener("input", update);
        // Initial call to set the counter to 0
        update();
    };

    // Initialize the counters for both text areas
    setupCounter(resumeInput, resumeCurrentChars, resumeCounter);
    setupCounter(jdInput, jdCurrentChars, jdCounter);


    // --- Event Listeners (rest of the file is the same) ---
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

        // UPDATED: Handle the response based on its status code
        if (!response.ok) {
            // NEW: Specifically check for the rate limit error
            if (response.status === 429) {
                const errorText = await response.text();
                dashboardPlaceholder.innerHTML = `<p style="color: #ff9800;">${errorText}</p>`;
                runButtonText.textContent = "Limit Reached"; // Keep button disabled
                // We don't re-enable the button in the finally block for this case
                return; // Exit the function
            }
            // Handle other server errors
            const errorText = await response.text();
            throw new Error(`HTTP error! status: ${response.status}, message: ${errorText}`);
        }
        
        const data = await response.json();
        updateDashboard(data);

    } catch (error) {
        console.error("Error fetching analysis:", error);
        dashboardPlaceholder.innerHTML = `<p style="color: #ff5555;">An error occurred. Please check the console and try again.</p>`;
    } finally {
        // This block will now only run for success or generic errors,
        // not for the 429 error because we 'return' out of the try block.
        if (runButtonText.textContent !== "Limit Reached") {
            runButton.disabled = false;
            runButtonText.textContent = "Analyze";
        }
    }
    });

    function markdownToHtml(text) {
      if (!text) return "";
      return text.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
    }

    function updateDashboard(data) {
        dashboardPlaceholder.classList.add("hidden");
        resultsContent.classList.remove("hidden");

        progressBarFill.style.width = `${data.matchScore}%`;
        scoreText.textContent = `${data.matchScore}%`;

        improvementsContent.innerHTML = markdownToHtml(data.improvements);
        nextStepsContent.innerHTML = markdownToHtml(data.nextSteps);
    }
});