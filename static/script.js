document.addEventListener("DOMContentLoaded", () => {
    // --- STATE ---
    let storedResume = "";
    let history = []; // Holds the array of analysis results
    const HISTORY_KEY = 'resumeAnalysisHistory';
    const MAX_HISTORY_ITEMS = 10;
    let isViewingHistory = false; // Tracks if we are viewing a historical result

    // --- ELEMENT SELECTORS ---
    // (All selectors are included for completeness)
    const resumeOverlay = document.getElementById("resume-overlay");
    const resumeInput = document.getElementById("resume-input");
    const saveResumeButton = document.getElementById("save-resume-button");
    const editResumeButton = document.getElementById("edit-resume-button");
    const jdForm = document.getElementById("jd-form");
    const jdInput = document.getElementById("jd-input");
    const runButton = document.getElementById("run-button");
    const runButtonText = document.getElementById("run-button-text");
    const newAnalysisButton = document.getElementById("new-analysis-button");
    const resumeCounter = document.getElementById("resume-char-counter");
    const resumeCurrentChars = document.getElementById("resume-current-chars");
    const jdCounter = document.getElementById("jd-char-counter");
    const jdCurrentChars = document.getElementById("jd-current-chars");
    const dashboardPlaceholder = document.getElementById("dashboard-placeholder");
    const resultsContent = document.getElementById("results-content");
    const progressBarFill = document.getElementById("progress-bar-fill");
    const scoreText = document.getElementById("score-text");
    const improvementsContent = document.getElementById("improvements-content");
    const nextStepsContent = document.getElementById("next-steps-content");
    const historyList = document.getElementById("history-list");
    const clearHistoryButton = document.getElementById("clear-history-button");

    // --- HISTORY FUNCTIONS ---
    const renderHistory = () => {
        historyList.innerHTML = ''; // Clear current list
        if (history.length === 0) {
            historyList.innerHTML = '<p style="text-align:center; color: #777; font-size: 0.9em; padding: 10px;">No history yet.</p>';
            return;
        }
        history.forEach((item, index) => {
            const historyItemEl = document.createElement('div');
            historyItemEl.className = 'history-item';
            historyItemEl.setAttribute('data-history-index', index);
            const date = new Date(item.timestamp);
            const formattedDate = `${date.toLocaleDateString()} ${date.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}`;
            historyItemEl.innerHTML = `<span class="history-item-score">${item.matchScore}%</span><span class="history-item-date">${formattedDate}</span>`;
            historyList.appendChild(historyItemEl);
        });
    };
    const loadHistory = () => {
        try {
            const storedHistory = localStorage.getItem(HISTORY_KEY);
            history = storedHistory ? JSON.parse(storedHistory) : [];
        } catch (e) { console.error("Could not parse history from localStorage", e); history = []; }
        renderHistory();
    };
    const saveToHistory = (newResult, jobDescription) => {
        const historyEntry = { ...newResult, timestamp: new Date().toISOString(), jobDescription: jobDescription };
        history.unshift(historyEntry);
        if (history.length > MAX_HISTORY_ITEMS) { history = history.slice(0, MAX_HISTORY_ITEMS); }
        localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
        renderHistory();
    };
    clearHistoryButton.addEventListener('click', () => {
        if (confirm('Are you sure you want to clear all analysis history?')) {
            history = [];
            localStorage.removeItem(HISTORY_KEY);
            renderHistory();
        }
    });

    // --- INITIALIZATION ---
    resumeOverlay.classList.add("visible");
    loadHistory();

    // --- CHARACTER COUNTERS ---
    const updateCounter = (textArea, currentEl, counterEl) => {
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
    const setupCounter = (textArea, currentEl, counterEl) => {
        textArea.addEventListener("input", () => updateCounter(textArea, currentEl, counterEl));
        updateCounter(textArea, currentEl, counterEl);
    };
    setupCounter(resumeInput, resumeCurrentChars, resumeCounter);
    setupCounter(jdInput, jdCurrentChars, jdCounter);

    // --- CORE EVENT LISTENERS ---
    saveResumeButton.addEventListener("click", () => {
        storedResume = resumeInput.value;
        resumeOverlay.classList.remove("visible");
        editResumeButton.classList.remove("hidden");
    });
    editResumeButton.addEventListener("click", () => { resumeOverlay.classList.add("visible"); });
    historyList.addEventListener('click', (event) => {
        const historyItemEl = event.target.closest('.history-item');
        if (!historyItemEl) return;
        const index = historyItemEl.getAttribute('data-history-index');
        const historicalData = history[index];
        if (!historicalData) return;
        isViewingHistory = true;
        document.querySelectorAll('.history-item').forEach(el => el.classList.remove('active'));
        historyItemEl.classList.add('active');
        updateDashboard(historicalData);
        jdInput.value = historicalData.jobDescription;
        jdInput.readOnly = true;
        updateCounter(jdInput, jdCurrentChars, jdCounter);
        runButton.classList.add('hidden');
        newAnalysisButton.classList.remove('hidden');
    });
    newAnalysisButton.addEventListener('click', () => {
        isViewingHistory = false;
        jdInput.value = '';
        jdInput.readOnly = false;
        updateCounter(jdInput, jdCurrentChars, jdCounter);
        dashboardPlaceholder.classList.remove('hidden');
        resultsContent.classList.add('hidden');
        runButton.classList.remove('hidden');
        newAnalysisButton.classList.add('hidden');
        document.querySelectorAll('.history-item').forEach(el => el.classList.remove('active'));
    });
    jdForm.addEventListener("submit", async (event) => {
        event.preventDefault();
        if (isViewingHistory) return;
        const jobDescriptionText = jdInput.value.trim();
        if (storedResume.trim() === "" || jobDescriptionText === "") {
            alert("Please provide both your resume and a job description.");
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
                body: JSON.stringify({ resume: storedResume, jobDescription: jobDescriptionText }),
            });
            if (!response.ok) {
                 if (response.status === 429) {
                    const errorText = await response.text();
                    dashboardPlaceholder.innerHTML = `<p style="color: #ff9800;">${errorText}</p>`;
                    runButtonText.textContent = "Limit Reached";
                    return;
                }
                const errorText = await response.text();
                throw new Error(`HTTP error! status: ${response.status}, message: ${errorText}`);
            }
            const data = await response.json();
            saveToHistory(data, jobDescriptionText);
            updateDashboard(data);
            document.querySelectorAll('.history-item').forEach(el => el.classList.remove('active'));
            if(historyList.firstChild && historyList.firstChild.classList) {
                historyList.firstChild.classList.add('active');
            }
        } catch (error) {
            console.error("Error fetching analysis:", error);
            dashboardPlaceholder.innerHTML = `<p style="color: #ff5555;">An error occurred. Please check the console and try again.</p>`;
        } finally {
            if (runButtonText.textContent !== "Limit Reached") {
                runButton.disabled = false;
                runButtonText.textContent = "Analyze";
            }
        }
    });

    // --- HELPER FUNCTIONS ---
    function markdownToHtml(text) {
        if (!text) return "";
        const lines = text.split('\n');
        const processedLines = lines.map(line => {
            const trimmedLine = line.trim();
            const startsWithBullet = /^[•*-]/.test(trimmedLine);
            if (trimmedLine.includes('**') && !startsWithBullet) {
                return '- ' + trimmedLine;
            }
            return line;
        });
        let processedText = processedLines.join('\n');
        return processedText
            .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
    }
    
    // UPDATED with console.log for debugging
    function updateDashboard(data) {
        console.log("--- Updating Dashboard ---");
        console.log("Received data object:", data);

        dashboardPlaceholder.classList.add("hidden");
        resultsContent.classList.remove("hidden");
        progressBarFill.style.width = `${data.matchScore}%`;
        scoreText.textContent = `${data.matchScore}%`;

        const improvementsText = Array.isArray(data.improvements) ? data.improvements.join('\n') : data.improvements;
        const nextStepsText = Array.isArray(data.nextSteps) ? data.nextSteps.join('\n') : data.nextSteps;

        console.log("Processed improvements text:", JSON.stringify(improvementsText));
        console.log("Processed next steps text:", JSON.stringify(nextStepsText));

        const finalImprovementsHtml = markdownToHtml(improvementsText);
        const finalNextStepsHtml = markdownToHtml(nextStepsText);

        console.log("Final improvements HTML:", finalImprovementsHtml);
        console.log("Final next steps HTML:", finalNextStepsHtml);

        improvementsContent.innerHTML = finalImprovementsHtml;
        nextStepsContent.innerHTML = finalNextStepsHtml;

        console.log("--- Dashboard Update Complete ---");
    }
});