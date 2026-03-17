import React, { useState } from "react";
import { format } from "https://esm.sh/date-fns@3.6.0";

type PageData = {
  CounterStart?: number;
  GeneratedAt?: string;
};

function Showcase() {
  const data = useBungoData() as PageData;
  const [count, setCount] = useState<number>(data.CounterStart ?? 0);
  const generatedAt = data.GeneratedAt
    ? format(new Date(data.GeneratedAt), "PPpp")
    : "unknown";

  return (
    <section className="card">
      <h2>TSX + Remote Import Demo</h2>
      <p>Counter starts at: {data.CounterStart ?? 0}</p>
      <p>Formatted with date-fns from esm.sh: {generatedAt}</p>
      <button
        type="button"
        className="btn"
        onClick={() => setCount((value) => value + 1)}
      >
        Clicked {count} times
      </button>
    </section>
  );
}

_bungoRender(Showcase);
