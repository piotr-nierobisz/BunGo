import React from "react";

function Loader() {
    const [status, setStatus] = React.useState("idle"); // idle | loading | image | error
    const [imageUrl, setImageUrl] = React.useState(null);
    const [errorMessage, setErrorMessage] = React.useState(null);

    async function handleClick() {
        setStatus("loading");
        setErrorMessage(null);
        try {
            const res = await fetch("/api/v1/constants", {
                headers: { Authorization: "pork-up" },
            });
            if (!res.ok) throw new Error("Failed to get constants");
            const data = await res.json();
            const url = data.url;
            if (!url) throw new Error("No url in response");

            const imgRes = await fetch(url);
            if (!imgRes.ok) throw new Error("Failed to load image");
            const blob = await imgRes.blob();
            const objectUrl = URL.createObjectURL(blob);
            setImageUrl(objectUrl);
            setStatus("image");
        } catch (err) {
            setErrorMessage(err.message || "Something went wrong");
            setStatus("error");
        }
    }

    return React.useMemo(() => {
        if (status === "image" && imageUrl) {
            return (
                <img
                    src={imageUrl}
                    alt="Loaded from API"
                    style={{ maxWidth: "100%", height: "auto", borderRadius: "8px" }}
                />
            );
        } else if (status === "error") {
            return (
                <div>
                    <p style={{ color: "#e57373", marginBottom: "1rem" }}>{errorMessage}</p>
                    <button
                        onClick={handleClick}
                        style={{
                            padding: "0.75rem 1.5rem",
                            fontSize: "1rem",
                            cursor: "pointer",
                            background: "#6366f1",
                            color: "#fff",
                            border: "none",
                            borderRadius: "8px",
                        }}
                    >
                        Retry
                    </button>
                </div>
            );
        } else {
            return (
                <button
                    onClick={handleClick}
                    disabled={status === "loading"}
                    style={{
                        padding: "0.75rem 1.5rem",
                        fontSize: "1rem",
                        cursor: status === "loading" ? "wait" : "pointer",
                        background: status === "loading" ? "#444" : "#6366f1",
                        color: "#fff",
                        border: "none",
                        borderRadius: "8px",
                    }}
                >
                    {status === "loading" ? "Loading…" : "Load image"}
                </button>
            )
        }
    }, [status, imageUrl, errorMessage]);
}

_bungoRender(Loader);
