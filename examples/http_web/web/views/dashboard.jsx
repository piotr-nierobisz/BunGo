import React from "react";

// Dashboard React app: reads server data from window.__BUNGO_DATA__ (injected by BunGo).
function Dashboard() {
    const [count, setCount] = React.useState(0);
    const [name, setName] = React.useState("");
    const { UserMessage, ServerTime } = window.__BUNGO_DATA__ || {};

    return (
        <div style={{
            padding: "1.25rem",
            background: "var(--surface-hover, #22222a)",
            borderRadius: "var(--radius, 10px)",
            border: "1px dashed var(--border, #2a2a32)",
        }}>
            <h2 style={{ marginTop: 0 }}>React component area</h2>
            <p className="muted">This block is managed by React (.jsx), bundled by BunGo with no Node/npm.</p>
            <div style={{ marginBottom: "1rem" }}>
                <p><strong>From server:</strong> {UserMessage} — {ServerTime}</p>
                <button className="btn" onClick={() => setCount((c) => c + 1)}>
                    Clicked {count} times
                </button>
            </div>
            <div>
                <input
                    type="text"
                    placeholder="Type your name..."
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    style={{
                        padding: "0.5rem 0.75rem",
                        borderRadius: "6px",
                        border: "1px solid var(--border)",
                        background: "var(--surface)",
                        color: "var(--text)",
                        width: "100%",
                        maxWidth: "240px",
                    }}
                />
                {name && <p style={{ color: "var(--accent)", marginTop: "0.5rem" }}>Hello, {name}!</p>}
            </div>
        </div>
    );
}

_bungoRender(Dashboard);
