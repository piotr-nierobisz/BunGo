import { useState } from "react";
import { createRoot } from "react-dom/client";

// Example: React component embedded in a Go-rendered page. Styling matches base layout (dark theme).
function LandingInteractive() {
    const [theme, setTheme] = useState("light");

    return (
        <div style={{
            padding: "1.5rem",
            background: theme === "light" ? "var(--surface, #1a1a20)" : "var(--surface-hover, #22222a)",
            borderRadius: "var(--radius, 10px)",
            marginTop: "1.5rem",
            border: "1px solid var(--border, #2a2a32)",
        }}>
            <h3 style={{ marginTop: 0 }}>Interactive React Section</h3>
            <p className="muted">React is seamlessly integrated into the same page as Go templates.</p>
            <button
                onClick={() => setTheme(theme === "light" ? "dark" : "light")}
                className="btn"
                style={{ marginTop: "0.5rem" }}
            >
                Toggle theme (current: {theme})
            </button>
        </div>
    );
}

const rootElement = document.getElementById("root");
if (rootElement) {
    const root = createRoot(rootElement);
    root.render(<LandingInteractive />);
}
