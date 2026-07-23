import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { retireServiceWorker } from "./retire-sw";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

// multimux no longer ships a service worker; clean up any older one still
// installed in this browser. See retire-sw.ts.
void retireServiceWorker();
