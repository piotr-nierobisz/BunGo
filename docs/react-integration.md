# React Integration

BunGo automatically embeds the React and ReactDOM environments straight into the Go binary. You write clean JSX and TSX as normal, but without bundlers hovering over your setup!

## The View File

All React code targets the `web/views/` folder and links to the `View` property to a `PageRoute`!

### Writing the Application

We eliminate complex boilerplates: Use the auto-injected `_bungoRender()` string to mount your application and the `useBungoData()` hook to read Go backend data directly inside the client! No more creating separate HTTP calls to fetch initial state!

```jsx
// inside web/views/loader.jsx

import React from "react";

function App() {
    // 1. Instantly read data sent from the Go Handler
    const serverData = useBungoData();
    
    return (
        <div>
            <h1>{serverData.PageTitle}</h1>
            <ul>
                {serverData.InitialData.map((item, idx) => (
                    <li key={idx}>{item}</li>
                ))}
            </ul>
        </div>
    );
}

// 2. Mount it! We assume <div id="root"></div> is in your .gohtml template.
_bungoRender(App);
```

### ESM Remote Imports
Since there is no `node_modules` folder, BunGo supports bringing in third-party dependencies through seamless Deno-style URL imports straight from CDNs. BunGo fetches and resolves them at build time, and **automatically caches them on disk** (in your global `os.UserCacheDir()/bungo/remote_modules` directory) to ensure blazing-fast restarts without re-downloading dependencies!

```jsx
// We can use Recharts natively just by specifying its ESM.sh link!
import { LineChart, Line, XAxis, YAxis } from "https://esm.sh/recharts@2.12.0";

// Recharts will automatically hook into the embedded React library under the hood!
function Chart() {
    // ...
}
```

### Importing Local Components
Just like in a traditional Node.js environment, you can break down your user interface into smaller, reusable components. Create a directory such as `web/components/` (the `bungo init` scaffold does not add this folder—you add it when you need shared components), export your modules, and import them from view entry files using relative paths.

```jsx
// web/components/Button.jsx
import React from "react";

export function Button({ label, onClick }) {
    return (
        <button onClick={onClick} style={{ background: "blue", color: "white" }}>
            {label}
        </button>
    );
}
```

```jsx
// web/views/loader.jsx
import React from "react";
import { Button } from "../components/Button.jsx";

function App() {
    const data = useBungoData();
    
    return (
        <div>
            <h1>{data.PageTitle}</h1>
            <Button label="Click Me!" onClick={() => alert("Ready!")} />
        </div>
    );
}

_bungoRender(App);
```

Next: [Security Layers](./security-layers.md).
